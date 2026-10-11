//go:build windows

package proc

import (
        "fmt"
        "strings"
        "syscall"
        "unsafe"

        "golang.org/x/sys/windows"
)

const (
        // TCP_TABLE_OWNER_PID_LISTENER exposes listener sockets with PIDs.
        tcpTableOwnerPIDListener = 3
)

var (
        iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
        procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

// tcpRowOwnerPid mirrors MIB_TCPROW_OWNER_PID (unsafe-backed; layout
// fixed by the OS: DWORD state, 4x DWORD addrs/ports, DWORD pid).
type tcpRowOwnerPid struct {
        State      uint32
        LocalAddr  uint32
        LocalPort  uint32
        RemoteAddr uint32
        RemotePort uint32
        OwningPID  uint32
}

func listeningProcessImpl(port int) (int, string, bool, error) {
        // Two-pass GetExtendedTcpTable (iphlpapi): size query, then fetch.
        //family AF_INET=2, class TCP_TABLE_OWNER_PID_LISTENER=3, reserved 0.
        getTable := func(buf []byte, size *uint32) error {
                var bufPtr *byte
                if len(buf) > 0 {
                        bufPtr = &buf[0]
                }

                r1, _, _ := procGetExtendedTcpTable.Call(
                        uintptr(unsafe.Pointer(bufPtr)),
                        uintptr(unsafe.Pointer(size)),
                        0, // bOrder: FALSE (order is irrelevant for a linear scan)
                        2, // AF_INET
                        tcpTableOwnerPIDListener,
                        0,
                )

                if r1 == windows.NO_ERROR {
                        return nil
                }

                return syscall.Errno(r1)
        }

        var size uint32
        err := getTable(nil, &size)
        if err != nil && err != syscall.ERROR_INSUFFICIENT_BUFFER {
                return 0, "", false, fmt.Errorf("tcp table size query: %w", err)
        }

        if size == 0 {
                return 0, "", false, nil
        }

        buf := make([]byte, size)
        if err := getTable(buf, &size); err != nil {
                return 0, "", false, fmt.Errorf("tcp table fetch: %w", err)
        }

        if len(buf) < 4 {
                return 0, "", false, nil
        }

        n := *(*uint32)(unsafe.Pointer(&buf[0]))
        rowSize := uint32(unsafe.Sizeof(tcpRowOwnerPid{}))

        // dwLocalPort stores the port in NETWORK byte order and only the
        // lower 16 bits are meaningful — "The maximum size of an IP port
        // number is 16 bits, so only the lower 16 bits should be used. The
        // upper 16 bits may contain uninitialized data" (MSDN,
        // MIB_TCPROW_OWNER_PID). The v1.3.6 code compared the raw DWORD
        // against port<<8, which placed the value in the wrong bits and
        // matched nothing above port 255 — every attribution on Windows
        // silently reported "no listener" (run 35996462352: the
        // self-owned-listener and both adoption tests failed).
        wanted := uint16((port&0xff)<<8) | uint16(port>>8)

        have := false
        var pid int

        for i := uint32(0); i < n; i++ {
                off := 4 + i*rowSize
                if off+rowSize > uint32(len(buf)) {
                        break
                }

                row := (*tcpRowOwnerPid)(unsafe.Pointer(&buf[off]))

                if uint16(row.LocalPort&0xFFFF) == wanted {
                        pid = int(row.OwningPID)
                        have = true
                        break
                }
        }

        if !have {
                return 0, "", false, nil
        }

        exe, err := processExePathImpl(pid)
        if err != nil {
                return pid, "", true, fmt.Errorf("listener pid %d image path: %w", pid, err)
        }

        return pid, exe, true, nil
}

func processExePathImpl(pid int) (string, error) {
        h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
        if err != nil {
                return "", fmt.Errorf("%w (pid %d): %v", ErrProcessNotFound, pid, err)
        }
        defer windows.CloseHandle(h)

        var buf [windows.MAX_LONG_PATH]uint16
        n := uint32(len(buf))
        if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
                return "", fmt.Errorf("query image name (pid %d): %w", pid, err)
        }

        return windows.UTF16ToString(buf[:n]), nil
}

func sameExecutableImpl(a, b string) bool {
        return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
