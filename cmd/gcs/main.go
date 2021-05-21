// +build linux

package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"syscall"
	"time"

	"github.com/Microsoft/hcsshim/internal/guest/bridge"
	"github.com/Microsoft/hcsshim/internal/guest/kmsg"
	"github.com/Microsoft/hcsshim/internal/guest/runtime/hcsv2"
	"github.com/Microsoft/hcsshim/internal/guest/runtime/runc"
	"github.com/Microsoft/hcsshim/internal/guest/transport"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/containerd/cgroups"
	cgroupstats "github.com/containerd/cgroups/stats/v1"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

func memoryLogFormat(metrics *cgroupstats.Metrics) logrus.Fields {
	return logrus.Fields{
		"memoryUsage":      metrics.Memory.Usage.Usage,
		"memoryUsageMax":   metrics.Memory.Usage.Max,
		"memoryUsageLimit": metrics.Memory.Usage.Limit,
		"swapUsage":        metrics.Memory.Swap.Usage,
		"swapUsageMax":     metrics.Memory.Swap.Max,
		"swapUsageLimit":   metrics.Memory.Swap.Limit,
		"kernelUsage":      metrics.Memory.Kernel.Usage,
		"kernelUsageMax":   metrics.Memory.Kernel.Max,
		"kernelUsageLimit": metrics.Memory.Kernel.Limit,
	}
}

func readMemoryEvents(startTime time.Time, efdFile *os.File, cgName string, threshold int64, cg cgroups.Cgroup) {
	// Buffer must be >= 8 bytes for eventfd reads
	// http://man7.org/linux/man-pages/man2/eventfd.2.html
	count := 0
	buf := make([]byte, 8)
	for {
		if _, err := efdFile.Read(buf); err != nil {
			logrus.WithError(err).WithField("cgroup", cgName).Error("failed to read from eventfd")
			return
		}

		// Sometimes an event is sent during cgroup teardown, but does not indicate that the
		// threshold was actually crossed. In the teardown case the cgroup.event_control file
		// won't exist anymore, so check that to determine if we should ignore this event.
		_, err := os.Lstat(fmt.Sprintf("/sys/fs/cgroup/memory%s/cgroup.event_control", cgName))
		if os.IsNotExist(err) {
			return
		}

		count++
		msg := "memory usage for cgroup exceeded threshold"
		entry := logrus.WithFields(logrus.Fields{
			"gcsStartTime":   startTime,
			"time":           time.Now(),
			"cgroup":         cgName,
			"thresholdBytes": threshold,
			"count":          count,
		})
		// Sleep for one second in case there is a series of allocations slightly after
		// reaching threshold.
		time.Sleep(time.Second)
		metrics, err := cg.Stat(cgroups.IgnoreNotExist)
		if err != nil {
			// Don't return on Stat err as it will return an error if
			// any of the cgroup subsystems Stat calls failed for any reason.
			// We still want to log if we hit the cgroup threshold/limit
			entry.WithError(err).Error(msg)
		} else {
			entry.WithFields(memoryLogFormat(metrics)).Warn(msg)
		}
	}
}

func main() {
	startTime := time.Now()
	logLevel := flag.String("loglevel", "debug", "Logging Level: debug, info, warning, error, fatal, panic.")
	coreDumpLoc := flag.String("core-dump-location", "", "The location/format where process core dumps will be written to.")
	kmsgLogLevel := flag.Uint("kmsgLogLevel", uint(kmsg.Warning), "Log all kmsg entries with a priority less than or equal to the supplied level.")
	logFile := flag.String("logfile", "", "Logging Target: An optional file name/path. Omit for console output.")
	logFormat := flag.String("log-format", "text", "Logging Format: text or json")
	useInOutErr := flag.Bool("use-inouterr", false, "If true use stdin/stdout for bridge communication and stderr for logging")
	v4 := flag.Bool("v4", false, "enable the v4 protocol support and v2 schema")
	rootMemReserveBytes := flag.Uint64("root-mem-reserve-bytes", 75*1024*1024, "the amount of memory reserved for the orchestration, the rest will be assigned to containers")
	gcsMemLimitBytes := flag.Uint64("gcs-mem-limit-bytes", 50*1024*1024, "the maximum amount of memory the gcs can use")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "\nUsage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "    %s -loglevel=debug -logfile=/run/gcs/gcs.log\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "    %s -loglevel=info -logfile=stdout\n", os.Args[0])
	}

	flag.Parse()

	// If v4 enable opencensus
	if *v4 {
		trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
		trace.RegisterExporter(&oc.LogrusExporter{})
	}

	// Use a file instead of stdout
	if *logFile != "" {
		logFileHandle, err := os.OpenFile(*logFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"path":          *logFile,
				logrus.ErrorKey: err,
			}).Fatal("failed to create log file")
		}
		logrus.SetOutput(logFileHandle)
	}

	switch *logFormat {
	case "text":
		// retain logrus's default.
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano, // include ns for accurate comparisons on the host
		})
	default:
		logrus.WithFields(logrus.Fields{
			"log-format": *logFormat,
		}).Fatal("unknown log-format")
	}

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}

	logrus.SetLevel(level)

	baseLogPath := "/run/gcs/c"

	logrus.Info("GCS started")

	// Set the process core dump location. This will be global to all containers as it's a kernel configuration.
	// If no path is specified core dumps will just be placed in the working directory of wherever the process
	// was invoked to a file named "core".
	if *coreDumpLoc != "" {
		if err := ioutil.WriteFile(
			"/proc/sys/kernel/core_pattern",
			[]byte(*coreDumpLoc),
			0644,
		); err != nil {
			logrus.WithError(err).Fatal("failed to set core dump location")
		}
	}

	// Continuously log /dev/kmsg
	go kmsg.ReadForever(kmsg.LogLevel(*kmsgLogLevel))

	tport := &transport.VsockTransport{}
	rtime, err := runc.NewRuntime(baseLogPath)
	if err != nil {
		logrus.WithError(err).Fatal("failed to initialize new runc runtime")
	}
	mux := bridge.NewBridgeMux()
	b := bridge.Bridge{
		Handler:  mux,
		EnableV4: *v4,
	}
	h := hcsv2.NewHost(rtime, tport)
	b.AssignHandlers(mux, h)

	var bridgeIn io.ReadCloser
	var bridgeOut io.WriteCloser
	if *useInOutErr {
		bridgeIn = os.Stdin
		bridgeOut = os.Stdout
	} else {
		const commandPort uint32 = 0x40000000
		bridgeCon, err := tport.Dial(commandPort)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"port":          commandPort,
				logrus.ErrorKey: err,
			}).Fatal("failed to dial host vsock connection")
		}
		bridgeIn = bridgeCon
		bridgeOut = bridgeCon
	}

	// Setup the UVM cgroups to protect against a workload taking all available
	// memory and causing the GCS to malfunction we create two cgroups: gcs,
	// containers.
	//

	// Write 1 to memory.use_hierarchy on the root cgroup to enable hierarchy
	// support. This needs to be set before we create any cgroups as the write
	// will fail otherwise.
	if err := ioutil.WriteFile("/sys/fs/cgroup/memory/memory.use_hierarchy", []byte("1"), 0644); err != nil {
		logrus.WithError(err).Fatal("failed to enable hierarchy support for root cgroup")
	}

	// The containers cgroup is limited only by {Totalram - 75 MB
	// (reservation)}.
	//
	// The gcs cgroup is not limited but an event will get logged if memory
	// usage exceeds 50 MB.
	sinfo := syscall.Sysinfo_t{}
	if err := syscall.Sysinfo(&sinfo); err != nil {
		logrus.WithError(err).Fatal("failed to get sys info")
	}
	containersLimit := int64(sinfo.Totalram - *rootMemReserveBytes)
	containersControl, err := cgroups.New(cgroups.V1, cgroups.StaticPath("/containers"), &oci.LinuxResources{
		Memory: &oci.LinuxMemory{
			Limit: &containersLimit,
		},
	})
	if err != nil {
		logrus.WithError(err).Fatal("failed to create containers cgroup")
	}
	defer containersControl.Delete()

	gcsControl, err := cgroups.New(cgroups.V1, cgroups.StaticPath("/gcs"), &oci.LinuxResources{})
	if err != nil {
		logrus.WithError(err).Fatal("failed to create gcs cgroup")
	}
	defer gcsControl.Delete()
	if err := gcsControl.Add(cgroups.Process{Pid: os.Getpid()}); err != nil {
		logrus.WithError(err).Fatal("failed add gcs pid to gcs cgroup")
	}

	event := cgroups.MemoryThresholdEvent(*gcsMemLimitBytes, false)
	gefd, err := gcsControl.RegisterMemoryEvent(event)
	if err != nil {
		logrus.WithError(err).Fatal("failed to register memory threshold for gcs cgroup")
	}
	gefdFile := os.NewFile(gefd, "gefd")
	defer gefdFile.Close()

	oom, err := containersControl.OOMEventFD()
	if err != nil {
		logrus.WithError(err).Fatal("failed to retrieve the container cgroups oom eventfd")
	}
	oomFile := os.NewFile(oom, "cefd")
	defer oomFile.Close()

	go readMemoryEvents(startTime, gefdFile, "/gcs", int64(*gcsMemLimitBytes), gcsControl)
	go readMemoryEvents(startTime, oomFile, "/containers", containersLimit, containersControl)
	err = b.ListenAndServe(bridgeIn, bridgeOut)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			logrus.ErrorKey: err,
		}).Fatal("failed to serve gcs service")
	}
}
