package hcs

import hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"

func (uvmb *utilityVMBuilder) SetStorageQos(iopsMaximum int64, bandwidthMaximum int64) error {
	uvmb.doc.VirtualMachine.StorageQoS = &hcsschema.StorageQoS{
		BandwidthMaximum: int32(bandwidthMaximum),
		IopsMaximum:      int32(iopsMaximum),
	}
	return nil
}
