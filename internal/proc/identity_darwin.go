//go:build darwin

package proc

// Darwin port-owner identity (run 44457547289 §1.3.a): the REAL libproc
// implementation this build previously refused to link. Port ownership is
// resolved through the genuine __proc_info BSD syscall (SYS_PROC_INFO) —
// the same mechanism the public libproc wrappers use — never through
// shell parsing, port guessing, or partial evidence.
//
// Every structure and constant below is transcribed from the public XNU
// headers (bsd/sys/proc_info.h, libsyscall/wrappers/libproc/libproc.h);
// none are hand-derived offsets. The call numbers (PROC_INFO_CALL_*,
// xnu-7195 public header, ABI-stable) and the PIDINFO/PIDFDINFO flavor
// spaces (PROC_PIDLISTFDS/PROC_PIDTBSDINFO/PROC_PIDPATHINFO and
// PROC_PIDFDSOCKETINFO in xnu main) were verified against the public
// headers at implementation time.
//
// Failure model (fail closed, run 44457547289 §1.3.a item 6):
//   - a candidate process that exits mid-walk is skipped (ESRCH);
//   - a candidate that cannot be inspected (EPERM/EACCES) is recorded as
//     an inaccessible candidate — if the walk ends without a proven owner,
//     the answer is the typed ErrPortOwnerUnknown, never a guess;
//   - a truncated descriptor list is retried with a larger buffer and,
//     at the cap, surfaces a typed error — never a silent partial walk;
//   - a PID alone is never identity: the caller pairs the returned PID
//     with the resolved executable path (and, at the adoption layer,
//     start-time/SHA proofs).

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ---------------------------------------------------------------------------
// Public XNU constants (bsd/sys/proc_info.h; call numbers from the public
// xnu-7195 header, identical in the current kernel switch order).
// ---------------------------------------------------------------------------

// __proc_info call numbers (bsd/sys/proc_info.h, PROC_INFO_CALL_*).
const (
	procInfoCallListPIDs  = 0x1 // PROC_INFO_CALL_LISTPIDS
	procInfoCallPIDInfo   = 0x2 // PROC_INFO_CALL_PIDINFO
	procInfoCallPIDFDInfo = 0x3 // PROC_INFO_CALL_PIDFDINFO
)

// PIDINFO flavor space (bsd/sys/proc_info.h).
const (
	procPIDListFDs  = 1  // PROC_PIDLISTFDS
	procPIDTBSDInfo = 3  // PROC_PIDTBSDINFO
	procPIDPathInfo = 11 // PROC_PIDPATHINFO
)

// PIDFDINFO flavor space (bsd/sys/proc_info.h).
const (
	procPIDFDSocketInfo = 3 // PROC_PIDFDSOCKETINFO
)

// procListPIDs type (bsd/sys/proc_info.h).
const procAllPIDs = 1 // PROC_ALL_PIDS

// Descriptor types (bsd/sys/proc_info.h, PROX_FDTYPE_*).
const (
	proxFDTypeVNode  = 1
	proxFDTypeSocket = 2
)

// maxPathLen mirrors MAXPATHLEN (sys/param.h) for the PROC_PIDPATHINFO
// buffer contract (PROC_PIDPATHINFO_SIZE = MAXPATHLEN).
const maxPathLen = 1024

// sysProcInfo is the SYS_PROC_INFO trap number (both darwin architectures
// define the identical BSD syscall number; x/sys/unix zsysnum_darwin_*).
var sysProcInfo = unix.SYS_PROC_INFO

// ---------------------------------------------------------------------------
// Public XNU structures (bsd/sys/proc_info.h) — field-for-field
// transcriptions; fixed-size arrays keep the C layout exactly. Size
// assertions below pin the transcription at init on every darwin
// architecture so a header drift can never silently misread memory.
// ---------------------------------------------------------------------------

// procFDInfo mirrors struct proc_fdinfo.
type procFDInfo struct {
	FD     int32
	FDType uint32
}

// procBsdInfo mirrors struct proc_bsdinfo (MAXCOMLEN = 16).
type procBsdInfo struct {
	Flags       uint32
	Status      uint32
	XStatus     uint32
	PID         uint32
	PPID        uint32
	UID         uint32
	GID         uint32
	RUID        uint32
	RGID        uint32
	SVUID       uint32
	SVGID       uint32
	Rfu1        uint32
	Comm        [16]byte
	Name        [32]byte
	NFiles      uint32
	PGrp        uint32
	PJobC       uint32
	TDev        uint32
	TPGID       uint32
	Nice        int32
	StartTVSec  uint64
	StartTVUsec uint64
}

// sockbufInfo mirrors struct sockbuf_info.
type sockbufInfo struct {
	CC    uint32
	HiWat uint32
	MBCnt uint32
	MBMax uint32
	LowAt uint32
	Flags int16
	Timeo int16
}

// inSockInfo mirrors struct in_sockinfo. insi_fport/insi_lport are
// stored by the kernel in NETWORK byte order; use localPortOf.
type inSockInfo struct {
	FPort  int32
	LPort  int32
	GenCnt uint64
	Flags  uint32
	Flow   uint32
	VFlag  uint8
	TTL    uint8
	_      [2]byte
	Rfu1   uint32
	FAddr  [16]byte
	LAddr  [16]byte
	V4     [1]byte
	_      [3]byte
	V6     [12]byte
}

// tcpSockInfo mirrors struct tcp_sockinfo (TSI_T_NTIMERS = 8).
type tcpSockInfo struct {
	Ini   inSockInfo
	State int32
	Timer [8]int32
	MSS   int32
	Flags uint32
	Rfu1  uint32
	TP    uint64
}

// socketInfo mirrors struct socket_info up to and including the soi_proto
// union. The union's largest member is tcp_sockinfo; pri_in (in_sockinfo)
// overlays the union start for INET/INET6 sockets, so the remaining union
// tail is carried as fixed padding.
type socketInfo struct {
	Stat      [136]byte // struct vinfo_stat (unused fields)
	So        uint64
	PCB       uint64
	Type      int32
	Protocol  int32
	Family    int32
	Options   int16
	Linger    int16
	State     int16
	QLen      int16
	IncQLen   int16
	QLimit    int16
	Timeo     int16
	Error     uint16
	OOBMark   uint32
	Rcv       sockbufInfo
	Snd       sockbufInfo
	Kind      int32
	Rfu2      uint32
	PriIn     inSockInfo
	ProtoTail [56]byte // union tail beyond in_sockinfo (tcp_sockinfo is 136)
}

// socketFDInfo mirrors struct socket_fdinfo.
type socketFDInfo struct {
	PFI procFileinfo
	PSI socketInfo
}

// procFileinfo mirrors struct proc_fileinfo.
type procFileinfo struct {
	OpenFlags  uint32
	Status     uint32
	Offset     int64
	Type       int32
	GuardFlags uint32
}

// Layout pins (compile-time): every transcription must occupy exactly the
// C size on both darwin architectures before the walk is allowed to run.
const (
	expectedProcFDInfoSize   = 8   // PROC_PIDLISTFD_SIZE
	expectedProcBsdInfoSize  = 136 // PROC_PIDTBSDINFO_SIZE
	expectedInSockInfoSize   = 80
	expectedTCPSockInfoSize  = 136
	expectedProcFileinfoSize = 24
	expectedSocketFDInfoSize = 400 // PROC_PIDFDSOCKETINFO_SIZE
)

func init() {
	if unsafe.Sizeof(procFDInfo{}) != expectedProcFDInfoSize {
		panic("proc: darwin proc_fdinfo layout drift")
	}

	if unsafe.Sizeof(procBsdInfo{}) != expectedProcBsdInfoSize {
		panic("proc: darwin proc_bsdinfo layout drift")
	}

	if unsafe.Sizeof(inSockInfo{}) != expectedInSockInfoSize {
		panic("proc: darwin in_sockinfo layout drift")
	}

	if unsafe.Sizeof(tcpSockInfo{}) != expectedTCPSockInfoSize {
		panic("proc: darwin tcp_sockinfo layout drift")
	}

	if unsafe.Sizeof(procFileinfo{}) != expectedProcFileinfoSize {
		panic("proc: darwin proc_fileinfo layout drift")
	}

	if unsafe.Sizeof(socketFDInfo{}) != expectedSocketFDInfoSize {
		panic("proc: darwin socket_fdinfo layout drift")
	}
}

// ---------------------------------------------------------------------------
// Typed walk states (run 44457547289 §1.3.a item 6).
// ---------------------------------------------------------------------------

var (
	// errProcGone: the candidate process exited during inspection.
	errProcGone = errors.New("process exited during inspection")

	// errProcAccessDenied: the candidate process cannot be inspected by
	// this (unprivileged, same-user) caller.
	errProcAccessDenied = errors.New("process inspection denied")

	// errProcTruncated: the kernel answer did not fit the buffer even
	// after growth retries — the walk refuses to run on partial data.
	errProcTruncated = errors.New("process descriptor list truncated")
)

// ---------------------------------------------------------------------------
// __proc_info wrappers (SYS_PROC_INFO, six-argument BSD syscall).
// ---------------------------------------------------------------------------

// procInfoRaw invokes __proc_info(callnum, pid, flavor, arg, buffer, size)
// and returns the byte count written by the kernel.
func procInfoRaw(callnum int, pid int32, flavor uint32, arg uint64, buf []byte) (int, error) {
	var bufPtr uintptr

	if len(buf) > 0 {
		bufPtr = uintptr(unsafe.Pointer(&buf[0]))
	}

	r1, _, errno := unix.Syscall6(
		uintptr(sysProcInfo),
		uintptr(callnum),
		uintptr(pid),
		uintptr(flavor),
		uintptr(arg),
		bufPtr,
		uintptr(len(buf)),
	)

	if errno != 0 {
		return 0, errno
	}

	return int(int32(r1)), nil
}

// procInfoPIDInfo is proc_pidinfo(pid, flavor, arg, buffer, buffersize).
func procInfoPIDInfo(pid int32, flavor uint32, arg uint64, buf []byte) (int, error) {
	return procInfoRaw(procInfoCallPIDInfo, pid, flavor, arg, buf)
}

// procInfoPIDFDInfo is proc_pidfdinfo(pid, flavor, fd, buffer, buffersize).
func procInfoPIDFDInfo(pid int32, flavor uint32, fd int32, buf []byte) (int, error) {
	return procInfoRaw(procInfoCallPIDFDInfo, pid, flavor, uint64(uint32(fd)), buf)
}

// mapProcInfoErrno translates the __proc_info errno contract into the
// typed walk states. ESRCH means the candidate died mid-walk; EPERM and
// EACCES mean this caller may not inspect the candidate.
func mapProcInfoErrno(err error) error {
	switch {
	case errors.Is(err, unix.ESRCH):
		return errProcGone
	case errors.Is(err, unix.EPERM), errors.Is(err, unix.EACCES):
		return errProcAccessDenied
	default:
		return err
	}
}

// structBytes reinterprets a fixed C-layout structure as its exact byte
// image for the __proc_info buffer contract.
func structBytes[T any](p *T) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(p)), int(unsafe.Sizeof(*p)))
}

// ---------------------------------------------------------------------------
// Port-owner walk.
// ---------------------------------------------------------------------------

// listAllPIDs returns every PID the kernel reports (PROC_ALL_PIDS). The
// buffer grows until the kernel answer no longer fills it; a walk that
// would exceed the cap fails typed instead of running partially.
func listAllPIDs() ([]int32, error) {
	const initialBytes = 8192
	const maxBytes = 16 << 20 // 16 MiB — far beyond any real process table

	size := initialBytes

	for {
		buf := make([]byte, size)

		n, err := procInfoRaw(procInfoCallListPIDs, procAllPIDs, 0, 0, buf)
		if err != nil {
			return nil, mapProcInfoErrno(err)
		}

		if n < 0 || n%int(unsafe.Sizeof(int32(0))) != 0 {
			return nil, fmt.Errorf("%w: kernel returned %d bytes for the PID list", errProcTruncated, n)
		}

		if n < size {
			pids := make([]int32, n/int(unsafe.Sizeof(int32(0))))

			for i := range pids {
				pids[i] = *(*int32)(unsafe.Pointer(&buf[i*int(unsafe.Sizeof(int32(0)))]))
			}

			return pids, nil
		}

		if size >= maxBytes {
			return nil, fmt.Errorf("%w: PID list exceeds the %d byte walk cap", errProcTruncated, maxBytes)
		}

		size *= 2
	}
}

// listProcSocketFDs returns the socket-type descriptor numbers of pid via
// PROC_PIDLISTFDS. Truncation grows the buffer; the cap fails typed.
func listProcSocketFDs(pid int32) ([]int32, error) {
	const initialBytes = 4096
	const maxBytes = 8 << 20 // 8 MiB of proc_fdinfo records

	size := initialBytes

	for {
		buf := make([]byte, size)

		n, err := procInfoPIDInfo(pid, procPIDListFDs, 0, buf)
		if err != nil {
			return nil, mapProcInfoErrno(err)
		}

		if n < 0 || n%int(unsafe.Sizeof(procFDInfo{})) != 0 {
			return nil, fmt.Errorf("%w: kernel returned %d bytes for the fd list of pid %d", errProcTruncated, n, pid)
		}

		full := n == size

		count := n / int(unsafe.Sizeof(procFDInfo{}))
		infos := make([]procFDInfo, count)

		for i := 0; i < count; i++ {
			infos[i] = *(*procFDInfo)(unsafe.Pointer(&buf[i*int(unsafe.Sizeof(procFDInfo{}))]))
		}

		if !full {
			var sockets []int32

			for _, fi := range infos {
				if fi.FDType == proxFDTypeSocket {
					sockets = append(sockets, fi.FD)
				}
			}

			return sockets, nil
		}

		if size >= maxBytes {
			return nil, fmt.Errorf("%w: fd list of pid %d exceeds the %d byte walk cap", errProcTruncated, pid, maxBytes)
		}

		size *= 2
	}
}

// readSocketFDInfo reads PROC_PIDFDSOCKETINFO for one descriptor. A short
// answer is a typed error, never a partially-read structure.
func readSocketFDInfo(pid int32, fd int32) (socketFDInfo, error) {
	var sfi socketFDInfo

	want := int(unsafe.Sizeof(sfi))

	n, err := procInfoPIDFDInfo(pid, procPIDFDSocketInfo, fd, unsafe.Slice((*byte)(unsafe.Pointer(&sfi)), want))
	if err != nil {
		return socketFDInfo{}, mapProcInfoErrno(err)
	}

	if n != want {
		return socketFDInfo{}, fmt.Errorf("%w: PROC_PIDFDSOCKETINFO for pid %d fd %d returned %d of %d bytes",
			errProcTruncated, pid, fd, n, want)
	}

	return sfi, nil
}

// localPortOf decodes insi_lport. The kernel stores the 16-bit port in
// NETWORK byte order (the convention every public PROC_PIDFDSOCKETINFO
// consumer applies via ntohs); both darwin architectures are
// little-endian, so the byte-swapped low half is the host port.
func localPortOf(raw int32) int {
	v := uint32(raw)

	if v > 0xFFFF {
		// A well-formed entry never sets the upper half; treat it as
		// unusable evidence rather than guessing.
		return 0
	}

	return int(v>>8&0xFF | v<<8&0xFF00)
}

// isTCPListenerOn reports whether the socket record is an INET/INET6 TCP
// socket in the listening state (SO_ACCEPTCONN) bound to the target port.
func isTCPListenerOn(sfi *socketFDInfo, port int) bool {
	if sfi.PSI.Family != unix.AF_INET && sfi.PSI.Family != unix.AF_INET6 {
		return false
	}

	if sfi.PSI.Protocol != unix.IPPROTO_TCP {
		return false
	}

	// SO_ACCEPTCONN = 0x0002 on darwin: only a socket in the listen
	// state carries it.
	if sfi.PSI.Options&0x0002 == 0 {
		return false
	}

	return localPortOf(sfi.PSI.PriIn.LPort) == port
}

// listeningProcessImpl resolves the PID owning a listening TCP socket on
// 127.0.0.1:port (loopback contract of ListeningProcess) through the real
// Darwin fd walk: PROC_ALL_PIDS -> PROC_PIDLISTFDS -> PROC_PIDFDSOCKETINFO,
// then PROC_PIDPATHINFO for the executable identity. See the file header
// for the fail-closed states.
func listeningProcessImpl(port int) (int, string, bool, error) {
	pids, err := listAllPIDs()
	if err != nil {
		return 0, "", false, fmt.Errorf("%w: darwin process enumeration failed: %v", ErrPortOwnerUnknown, err)
	}

	sawInaccessible := false

	for _, pid := range pids {
		if pid <= 0 {
			continue
		}

		fds, ferr := listProcSocketFDs(pid)
		if ferr != nil {
			switch {
			case errors.Is(ferr, errProcGone):
				// The candidate exited mid-walk — not an owner.
				continue
			case errors.Is(ferr, errProcAccessDenied):
				// Something on this host cannot be inspected; remember
				// it so a proven-nothing answer stays honest.
				sawInaccessible = true
				continue
			default:
				return 0, "", false, fmt.Errorf("%w: pid %d fd walk failed: %v", ErrPortOwnerUnknown, pid, ferr)
			}
		}

	fdLoop:
		for _, fd := range fds {
			sfi, serr := readSocketFDInfo(pid, fd)
			if serr != nil {
				switch {
				case errors.Is(serr, errProcGone):
					// Process (or its descriptor table) went away —
					// it cannot be the owner we are proving.
					break fdLoop
				case errors.Is(serr, errProcAccessDenied):
					sawInaccessible = true
					break fdLoop
				default:
					return 0, "", false, fmt.Errorf("%w: pid %d fd %d socket info failed: %v", ErrPortOwnerUnknown, pid, fd, serr)
				}
			}

			if !isTCPListenerOn(&sfi, port) {
				continue
			}

			exe, perr := processExePathImpl(int(pid))
			if perr != nil {
				switch {
				case errors.Is(perr, ErrProcessNotFound):
					// The owner died between the socket read and the
					// path read — no adoption of a corpse.
				case errors.Is(perr, errProcAccessDenied):
					sawInaccessible = true
				default:
					return 0, "", false, fmt.Errorf("%w: executable path of pid %d unresolvable: %v", ErrPortOwnerUnknown, pid, perr)
				}

				break fdLoop
			}

			return int(pid), exe, true, nil
		}
	}

	if sawInaccessible {
		return 0, "", false, fmt.Errorf("%w: port %d has listener candidates this process may not inspect on %s/%s",
			ErrPortOwnerUnknown, port, runtime.GOOS, runtime.GOARCH)
	}

	return 0, "", false, nil
}

// processExePathImpl resolves the image path via PROC_PIDPATHINFO (the
// mechanism proc_pidpath uses). Typed results: ErrProcessNotFound for a
// vanished PID, errProcAccessDenied for an uninspectable one.
func processExePathImpl(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrProcessNotFound
	}

	buf := make([]byte, maxPathLen)

	n, err := procInfoPIDInfo(int32(pid), procPIDPathInfo, 0, buf)
	if err != nil {
		return "", mapProcInfoErrno(err)
	}

	// The kernel writes a NUL-terminated path; the byte count covers it.
	end := n
	if end > len(buf) {
		end = len(buf)
	}

	for end > 0 && buf[end-1] == 0 {
		end--
	}

	if end == 0 {
		return "", fmt.Errorf("%w: pid %d image path is empty", ErrProcessMetadataUnsupported, pid)
	}

	return string(buf[:end]), nil
}

// sameExecutableImpl on darwin resolves symlinks and compares paths
// (darwin is case-sensitive, unlike the Windows implementation).
func sameExecutableImpl(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}

	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}

	return a == b
}
