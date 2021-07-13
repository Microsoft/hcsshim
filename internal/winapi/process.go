package winapi

const PROCESS_ALL_ACCESS uint32 = 0x1FFFFF

const (
	PROC_THREAD_ATTRIBUTE_JOB_LIST       uintptr = 0x2000D
	PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE  uintptr = 0x20016
	PROC_THREAD_ATTRIBUTE_PARENT_PROCESS uintptr = 0x20000
)

// BOOL CreateProcessAsUserW(
// 	HANDLE                hToken,
// 	LPCWSTR               lpApplicationName,
// 	LPWSTR                lpCommandLine,
// 	LPSECURITY_ATTRIBUTES lpProcessAttributes,
// 	LPSECURITY_ATTRIBUTES lpThreadAttributes,
// 	BOOL                  bInheritHandles,
// 	DWORD                 dwCreationFlags,
// 	LPVOID                lpEnvironment,
// 	LPCWSTR               lpCurrentDirectory,
// 	LPSTARTUPINFOW        lpStartupInfo,
// 	LPPROCESS_INFORMATION lpProcessInformation
// );
//sys CreateProcessAsUser(hToken windows.Token, appName *uint16, commandLine *uint16, procSecurity *windows.SecurityAttributes, threadSecurity *windows.SecurityAttributes, inheritHandles bool, creationFlags uint32, env *uint16, currentDir *uint16, startupInfo *windows.StartupInfo, outProcInfo *windows.ProcessInformation) (err error) = Advapi32.CreateProcessAsUserW
