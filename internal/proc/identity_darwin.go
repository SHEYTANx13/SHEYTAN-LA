//go:build darwin && !ios && cgo

package proc

// Darwin port-owner identity (run 44547031698 §5): an isolated Darwin
// libproc adapter — a small Darwin-only CGo bridge compiled against the
// ACTUAL macOS SDK headers (libproc.h + sys/proc_info.h).
//
// INTERFACE STABILITY (run 44835425398 repair): Apple's libproc.h states
// verbatim — "This header file contains private interfaces to obtain
// process information. These interfaces are subject to change in future
// releases." The functions this adapter calls are therefore NOT a stable
// public API and are never documented as one: this file is the single
// isolated consumer, every answer is validated against the shape the
// INSTALLED SDK headers describe, and a future macOS that changes the
// contract fails typed here instead of producing a guessed port owner.
// No supported public replacement is claimed to exist.
//
// The previous implementation hand-transcribed the XNU proc_info
// structures into Go and invoked the raw __proc_info SYS_PROC_INFO trap.
// Cross-compilation succeeded, but the native suites failed on both macOS
// architectures and the macOS-host iOS lane: a transcription can never be
// proven against the real kernel ABI (the PROC_PIDTBSDINFO flavor was
// transcribed as 3 — the real sys/proc_info.h value is 4, flavor 3 is
// PROC_PIDTASKALLINFO — and the socket_fdinfo layout was modeled with a
// hand-padded union tail that no header corroborates). This file removes
// every transcribed layout: all structure knowledge lives in C, compiled
// against libproc.h + sys/proc_info.h from the SDK, and Go receives only
// scalar answers — no Go-side code depends on an Apple struct layout.
//
// Socket-kind gate (run 44835425398 repair): the TCP local port is read
// ONLY through soi_proto.pri_tcp.tcpsi_ini.insi_lport and ONLY when
// soi_kind equals the SDK-defined SOCKINFO_TCP — the union member named
// by the previous code (soi_proto.insi) does not exist, which is exactly
// the compiler failure all three Darwin lanes reported. Every documented
// non-TCP kind (SOCKINFO_GENERIC, SOCKINFO_IN, SOCKINFO_UN, SOCKINFO_NDRV,
// SOCKINFO_KERN_EVENT, SOCKINFO_KERN_CTL) is decoded without touching any
// union member, and a soi_kind outside the SDK-documented set fails the
// walk through the typed undocumented-kind error — never interpreted.
//
// Adapter surface (libproc.h declarations):
//   proc_listallpids  — every PID (pid-count return)
//   proc_pidinfo      — PROC_PIDLISTFDS (fd walk, byte-count return)
//                       PROC_PIDTBSDINFO (start identity, byte-count return)
//   proc_pidfdinfo    — PROC_PIDFDSOCKETINFO (socket record, byte-count return)
//   proc_pidpath      — executable image path (path-length return)
//
// Failure model (fail closed, work order §5.1):
//   - a candidate that exits mid-walk is skipped (ESRCH → typed gone);
//   - a candidate that cannot be inspected is recorded as inaccessible —
//     if the walk ends without a proven owner the answer is the typed
//     ErrPortOwnerUnknown, never a guess;
//   - a truncated list is retried with a larger buffer and, at the cap,
//     surfaces a typed error — never a silent partial walk;
//   - a socket record whose kind is not a documented SDK kind fails the
//     walk typed — never interpreted through a guessed union view;
//   - every returned byte count is validated against the ABI contract
//     (kernel-ambiguous answers fail closed, never reinterpreted);
//   - a PID alone is never identity: the caller pairs the returned PID
//     with the resolved executable path (and, at the adoption layer,
//     start-time/SHA proofs);
//   - no Go pointer is retained in C: buffers are passed for the duration
//     of one call only and pinned with runtime.KeepAlive.

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

/*
#include <errno.h>
#include <libproc.h>
#include <sys/param.h>
#include <sys/proc_info.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <string.h>

// c_pid_t_size / c_proc_fdinfo_size / c_socket_fdinfo_size /
// c_bsdinfo_size / c_pathinfo_size report the REAL SDK sizes — the Go
// side never hard-codes a layout byte count.
static int c_pid_t_size(void)          { return (int)sizeof(pid_t); }
static int c_proc_fdinfo_size(void)    { return (int)sizeof(struct proc_fdinfo); }
static int c_socket_fdinfo_size(void)  { return (int)sizeof(struct socket_fdinfo); }
static int c_bsdinfo_size(void)        { return (int)sizeof(struct proc_bsdinfo); }
static int c_pathinfo_size(void)       { return (int)PROC_PIDPATHINFO_SIZE; }

// NOTE (run 44663205295 repair): the previous prelude included
// <sys/limits.h> and read PID_MAX through a C helper. Neither exists in
// the public Apple SDK: sys/limits.h is a BSD header the macOS SDK does
// not ship (fatal error: 'sys/limits.h' file not found) and PID_MAX
// lives in the XNU-PRIVATE sys/proc_internal.h. The pid-space upper
// bound therefore lives on the Go side as the explicit typed constant
// darwinPIDSpaceUpperBound below — the walk stays bounded by an
// explicit maximum allocation with a typed truncation error instead of
// depending on a private or unavailable macro.

// c_list_all_pids fills buf with every PID (PROC_ALL_PIDS) through the
// proc_listallpids wrapper. Returns the pid COUNT (the ABI contract of
// proc_listallpids), or -1 with errno set.
static int c_list_all_pids(void *buf, int cap_pids) {
        return proc_listallpids(buf, cap_pids * (int)sizeof(pid_t));
}

// c_pid_at reads one decoded pid from the list buffer.
static int c_pid_at(const void *buf, int index) {
        const pid_t *pids = (const pid_t *)buf;
        return (int)pids[index];
}

// c_list_fds returns the PROC_PIDLISTFDS answer in BYTES for pid
// (the documented proc_pidinfo byte-count contract), or -1 with errno set.
static int c_list_fds(int pid, void *buf, int bytes) {
        return (int)proc_pidinfo(pid, PROC_PIDLISTFDS, 0, buf, bytes);
}

// c_fd_at / c_fdtype_at decode one proc_fdinfo record from the list buffer
// through the real header structure — no Go-side layout knowledge.
static int c_fd_at(const void *buf, int index) {
        const struct proc_fdinfo *fds = (const struct proc_fdinfo *)buf;
        return (int)fds[index].proc_fd;
}

static int c_fdtype_at(const void *buf, int index) {
        const struct proc_fdinfo *fds = (const struct proc_fdinfo *)buf;
        return (int)fds[index].proc_fdtype;
}

// c_prox_fdtype_socket is the real sys/proc_info.h descriptor type for
// sockets (PROX_FDTYPE_SOCKET).
static int c_prox_fdtype_socket(void) { return PROX_FDTYPE_SOCKET; }

// c_socket_fdinfo fetches the PROC_PIDFDSOCKETINFO record for one
// descriptor. Returns the byte count written (must equal sizeof(struct
// socket_fdinfo)), or -1 with errno set.
static int c_socket_fdinfo(int pid, int fd, void *buf, int bytes) {
        return (int)proc_pidfdinfo(pid, fd, PROC_PIDFDSOCKETINFO, buf, bytes);
}

// c_socket_fields extracts ONLY the scalar fields the listener decision
// needs, from the REAL header structures, and reports which union view the
// record actually carries (run 44835425398 repair: the previous version
// read soi_proto.insi — a member that does not exist in the header union;
// the TCP local port lives at soi_proto.pri_tcp.tcpsi_ini.insi_lport and
// that path is valid ONLY when soi_kind is the SDK's SOCKINFO_TCP).
// Status codes:
//   0  decoded — soi_kind is SOCKINFO_TCP; raw_lport was read through
//      pri_tcp.tcpsi_ini.insi_lport, exactly as the kernel stores it
//      (network byte order in the low half — the Go side owns the ntohs
//      convention and its regression test).
//   1  decoded — soi_kind is a documented NON-TCP kind (SOCKINFO_GENERIC,
//      SOCKINFO_IN, SOCKINFO_UN, SOCKINFO_NDRV, SOCKINFO_KERN_EVENT,
//      SOCKINFO_KERN_CTL); family/protocol/options/kind are the real
//      header values and raw_lport is the explicit -1 non-port sentinel —
//      no union member was interpreted as TCP data.
//  -1  soi_kind is outside every kind the installed SDK documents — the
//      record cannot be interpreted; the Go side fails closed on the
//      typed undocumented-kind error.
static int c_socket_fields(const void *buf, int *family, int *protocol, int *options, int *kind, int *raw_lport) {
        const struct socket_fdinfo *sfi = (const struct socket_fdinfo *)buf;
        *family    = (int)sfi->psi.soi_family;
        *protocol  = (int)sfi->psi.soi_protocol;
        *options   = (int)sfi->psi.soi_options;
        *kind      = (int)sfi->psi.soi_kind;

        switch (sfi->psi.soi_kind) {
        case SOCKINFO_TCP:
                *raw_lport = (int)sfi->psi.soi_proto.pri_tcp.tcpsi_ini.insi_lport;
                return 0;
        case SOCKINFO_GENERIC:
        case SOCKINFO_IN:
        case SOCKINFO_UN:
        case SOCKINFO_NDRV:
        case SOCKINFO_KERN_EVENT:
        case SOCKINFO_KERN_CTL:
                // A real, documented non-TCP kind: the TCP union member
                // MUST NOT be read — mark the record with the non-port
                // sentinel so no value can be mistaken for a port.
                *raw_lport = -1;
                return 1;
        default:
                return -1;
        }
}

// c_so_acceptconn is the real sys/socket.h value of SO_ACCEPTCONN: only a
// socket in the listen state carries it.
static int c_so_acceptconn(void) { return SO_ACCEPTCONN; }

// c_af_inet / c_af_inet6 / c_ipproto_tcp are the real header constants
// for the INET listener contract.
static int c_af_inet(void)      { return AF_INET; }
static int c_af_inet6(void)     { return AF_INET6; }
static int c_ipproto_tcp(void)  { return IPPROTO_TCP; }

// c_proc_path resolves the image path through proc_pidpath (the libproc.h
// wrapper). Returns the path length (>0), 0 when the process has no
// resolvable path, or -1 with errno set.
static int c_proc_path(int pid, void *buf, int bytes) {
        return (int)proc_pidpath(pid, buf, (uint32_t)bytes);
}

// c_start_time fetches PROC_PIDTBSDINFO and returns the absolute epoch
// start identity (pbi_start_tvsec / pbi_start_tvusec) — the PID-reuse
// defense values, read through the real header structure. Return codes:
//   0  success (sec/usec filled);
//   1  proc_pidinfo answered SHORT without a kernel errno — the Go side
//      turns this into a typed error, never a fabricated timestamp;
//  -1  proc_pidinfo failed with errno set (ESRCH for a dead pid,
//      EPERM/EACCES for an uninspectable one).
static int c_start_time(int pid, long long *sec, long long *usec) {
        struct proc_bsdinfo bsd;
        memset(&bsd, 0, sizeof(bsd));
        errno = 0;
        int n = (int)proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bsd, (int)sizeof(bsd));
        if (n == (int)sizeof(bsd)) {
                *sec  = (long long)bsd.pbi_start_tvsec;
                *usec = (long long)bsd.pbi_start_tvusec;
                return 0;
        }
        if (n < 0) {
                return -1;
        }
        errno = 0;
        return 1;
}
*/
import "C"

// darwinIdentityNative reports that this build links the real libproc
// bridge (the native-walk test contract applies; the CGO_ENABLED=0 and
// ios builds carry the typed-unsupported provider instead).
const darwinIdentityNative = true

// Typed walk states (fail-closed contract).
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

// errSocketKindUndocumented: a socket record's soi_kind is outside every
// kind the installed SDK documents — the union layout is unknown, so the
// walk refuses the record instead of interpreting any union member as TCP
// data (run 44835425398 repair: fail closed, never a guessed port).
var errSocketKindUndocumented = errors.New("socket record carries an undocumented kind")

// mapProcInfoErrno translates the libproc errno contract into the typed
// walk states. ESRCH means the candidate died mid-walk; EPERM and EACCES
// mean this caller may not inspect the candidate.
// The errno constants come from the stdlib syscall package (typed
// syscall.Errno): a bare C.ESRCH is an UNTYPED cgo constant and would
// never match the syscall.Errno cgo returns inside errors.Is.
func mapProcInfoErrno(err error) error {
	switch {
	case errors.Is(err, syscall.ESRCH):
		return errProcGone
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return errProcAccessDenied
	default:
		return err
	}
}

// darwinPIDSpaceUpperBound is the kernel pid space upper bound the walk
// validates every decoded pid against. The value mirrors the documented
// XNU historical PID_MAX (99999): the constant CANNOT be read from a
// public Apple SDK header (sys/limits.h is not shipped on macOS and
// PID_MAX lives in the XNU-private sys/proc_internal.h — run
// 44663205295 repair), so it is carried here as an explicit, typed
// bound instead of depending on a private or unavailable macro. A pid
// outside [1, darwinPIDSpaceUpperBound] means the buffer decode is
// wrong and the walk fails typed — never a silent partial answer.
const darwinPIDSpaceUpperBound = 99999

// listAllPIDs returns every PID the kernel reports (PROC_ALL_PIDS) via
// proc_listallpids. The buffer grows until the documented pid-count answer
// no longer fills it; every returned pid is validated, and an answer that
// cannot fit the documented contract fails typed instead of guessing.
func listAllPIDs() ([]int32, error) {
	const initialPIDs = 8192
	const maxPIDs = 1 << 21 // 2,097,152 pids — far beyond any real process table

	pidSize := int(C.c_pid_t_size())
	capPIDs := initialPIDs

	for {
		buf := make([]byte, capPIDs*pidSize)

		n, err := C.c_list_all_pids(unsafe.Pointer(&buf[0]), C.int(capPIDs))
		runtime.KeepAlive(buf)

		if err != nil {
			return nil, mapProcInfoErrno(err)
		}

		if n < 0 || int(n) > capPIDs {
			return nil, fmt.Errorf("%w: proc_listallpids answered %d against a %d-pid buffer — the kernel answer does not fit the documented pid-count contract", errProcTruncated, n, capPIDs)
		}

		if int(n) < capPIDs {
			pids := make([]int32, 0, int(n))

			for i := 0; i < int(n); i++ {
				p := int(C.c_pid_at(unsafe.Pointer(&buf[0]), C.int(i)))
				runtime.KeepAlive(buf)

				// Semantic validation: a pid outside [1,
				// darwinPIDSpaceUpperBound] means the buffer decode is
				// wrong — fail closed, never guess. The bound is the
				// explicit Go-side constant above (the public Apple SDK
				// does not ship sys/limits.h, and XNU's PID_MAX is
				// private — run 44663205295 repair).
				if p <= 0 || p > darwinPIDSpaceUpperBound {
					return nil, fmt.Errorf("%w: proc_listallpids returned pid %d at index %d — outside the documented pid space", errProcTruncated, p, i)
				}

				pids = append(pids, int32(p))
			}

			return pids, nil
		}

		if capPIDs >= maxPIDs {
			return nil, fmt.Errorf("%w: the pid list exceeds the %d-pid walk cap", errProcTruncated, maxPIDs)
		}

		capPIDs *= 2
	}
}

// listProcSocketFDs returns the socket-type descriptor numbers of pid via
// PROC_PIDLISTFDS. proc_pidinfo answers in BYTES (the documented
// contract); truncation grows the buffer and the cap fails typed.
func listProcSocketFDs(pid int32) ([]int32, error) {
	const initialBytes = 4096
	const maxBytes = 8 << 20 // 8 MiB of proc_fdinfo records

	recSize := int(C.c_proc_fdinfo_size())
	fdSocket := int(C.c_prox_fdtype_socket())

	size := initialBytes

	for {
		buf := make([]byte, size)

		n, err := C.c_list_fds(C.int(pid), unsafe.Pointer(&buf[0]), C.int(size))
		runtime.KeepAlive(buf)

		if err != nil {
			return nil, mapProcInfoErrno(err)
		}

		if n < 0 || int(n) > size {
			return nil, fmt.Errorf("%w: PROC_PIDLISTFDS for pid %d returned %d bytes against a %d-byte buffer", errProcTruncated, pid, n, size)
		}

		full := int(n) == size

		if int(n)%recSize != 0 {
			return nil, fmt.Errorf("%w: PROC_PIDLISTFDS for pid %d returned %d bytes — not a whole proc_fdinfo record count", errProcTruncated, pid, n)
		}

		count := int(n) / recSize
		var sockets []int32

		for i := 0; i < count; i++ {
			fd := int(C.c_fd_at(unsafe.Pointer(&buf[0]), C.int(i)))
			fdtype := int(C.c_fdtype_at(unsafe.Pointer(&buf[0]), C.int(i)))
			runtime.KeepAlive(buf)

			// Semantic validation: descriptors and fd types live in the
			// documented kernel spaces; anything else means the record
			// decode is wrong — fail closed.
			if fd < 0 {
				return nil, fmt.Errorf("%w: PROC_PIDLISTFDS for pid %d returned descriptor %d at index %d", errProcTruncated, pid, fd, i)
			}

			if fdtype == fdSocket {
				sockets = append(sockets, int32(fd))
			}
		}

		if !full {
			return sockets, nil
		}

		if size >= maxBytes {
			return nil, fmt.Errorf("%w: the fd list of pid %d exceeds the %d-byte walk cap", errProcTruncated, pid, maxBytes)
		}

		size *= 2
	}
}

// socketRecord is the scalar projection of ONE PROC_PIDFDSOCKETINFO record
// the listener decision needs. The struct layout itself never leaves C.
// KindIsTCP is the typed gate produced by the C extractor: only a record
// whose soi_kind is the SDK's SOCKINFO_TCP is interpreted through the
// union at all — a non-TCP record's RawLPort is the -1 non-port sentinel
// and never a decoded port.
type socketRecord struct {
	Family    int32
	Protocol  int32
	Options   int32
	RawLPort  int32 // TCP-kind records: kernel network byte order in the low half; non-TCP kinds: -1 sentinel
	KindIsTCP bool  // soi_kind == SOCKINFO_TCP (the only kind whose union view is interpreted)
}

// readSocketFDInfo reads PROC_PIDFDSOCKETINFO for one descriptor through
// proc_pidfdinfo. A short or over-long answer is a typed error — never a
// partially-read record. The C extractor's status is enforced here: 0 =
// SOCKINFO_TCP record (union view interpreted), 1 = documented non-TCP
// kind (decoded, no union interpretation, RawLPort -1 sentinel), anything
// else = undocumented kind → typed fail-closed error (run 44835425398
// repair: an invalid kind is never turned into port zero or a guess).
func readSocketFDInfo(pid int32, fd int32) (socketRecord, error) {
	want := int(C.c_socket_fdinfo_size())
	buf := make([]byte, want)

	n, err := C.c_socket_fdinfo(C.int(pid), C.int(fd), unsafe.Pointer(&buf[0]), C.int(want))
	runtime.KeepAlive(buf)

	if err != nil {
		return socketRecord{}, mapProcInfoErrno(err)
	}

	if int(n) != want {
		return socketRecord{}, fmt.Errorf("%w: PROC_PIDFDSOCKETINFO for pid %d fd %d returned %d of %d bytes",
			errProcTruncated, pid, fd, n, want)
	}

	var (
		family   C.int
		protocol C.int
		options  C.int
		kind     C.int
		rawLPort C.int
	)

	status, err := C.c_socket_fields(unsafe.Pointer(&buf[0]), &family, &protocol, &options, &kind, &rawLPort)
	runtime.KeepAlive(buf)

	if err != nil {
		return socketRecord{}, mapProcInfoErrno(err)
	}

	switch {
	case status == 0:
		// SOCKINFO_TCP record: the only union view the walk interprets.
	case status == 1:
		// A documented non-TCP kind: decoded, never union-interpreted,
		// RawLPort carries the -1 non-port sentinel.
	default:
		return socketRecord{}, fmt.Errorf("%w: PROC_PIDFDSOCKETINFO for pid %d fd %d carries socket kind %d, outside every kind the installed SDK documents — the record is not interpretable and the walk fails closed",
			errSocketKindUndocumented, pid, fd, int(kind))
	}

	return socketRecord{
		Family:    int32(family),
		Protocol:  int32(protocol),
		Options:   int32(options),
		RawLPort:  int32(rawLPort),
		KindIsTCP: status == 0,
	}, nil
}

// isTCPListenerOn reports whether the socket record is an INET/INET6 TCP
// socket in the listening state (SO_ACCEPTCONN, the real sys/socket.h
// value) bound to the target port. The kind gate comes first: only a
// record the C extractor proved to be SOCKINFO_TCP may be considered at
// all — family/protocol/options alone never justify interpreting a record
// as TCP data (run 44835425398 repair).
func isTCPListenerOn(rec *socketRecord, port int) bool {
	if !rec.KindIsTCP {
		return false
	}

	if rec.Family != int32(C.c_af_inet()) && rec.Family != int32(C.c_af_inet6()) {
		return false
	}

	if rec.Protocol != int32(C.c_ipproto_tcp()) {
		return false
	}

	if rec.Options&int32(C.c_so_acceptconn()) == 0 {
		return false
	}

	return localPortOf(rec.RawLPort) == port
}

// listeningProcessImpl resolves the PID owning a listening TCP socket on
// 127.0.0.1:port (loopback contract of ListeningProcess) through the real
// Darwin fd walk: PROC_ALL_PIDS -> PROC_PIDLISTFDS -> PROC_PIDFDSOCKETINFO,
// then proc_pidpath for the executable identity. See the file header for
// the fail-closed states.
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
			rec, serr := readSocketFDInfo(pid, fd)
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

			if !isTCPListenerOn(&rec, port) {
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

// processExePathImpl resolves the image path via proc_pidpath (the
// libproc.h declaration — private interface, see the file header). Typed
// results: ErrProcessNotFound for a vanished or path-less pid,
// errProcAccessDenied for an uninspectable one.
func processExePathImpl(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrProcessNotFound
	}

	pathSize := int(C.c_pathinfo_size())
	buf := make([]byte, pathSize)

	n, err := C.c_proc_path(C.int(pid), unsafe.Pointer(&buf[0]), C.int(pathSize))
	runtime.KeepAlive(buf)

	if err != nil {
		// Run 45086860202 repair: a vanished PID surfaces the PUBLIC
		// ErrProcessNotFound sentinel — exactly like processStartTimeImpl
		// already does — never the walk-internal errProcGone class.
		// TestListeningProcessDeadOwnerRefused pins the public contract
		// (errors.Is(err, ErrProcessNotFound)) and the native darwin suite
		// finally executed it here; the raw errno passthrough broke it.
		switch mapped := mapProcInfoErrno(err); {
		case errors.Is(mapped, errProcGone):
			return "", ErrProcessNotFound
		case errors.Is(mapped, errProcAccessDenied):
			return "", fmt.Errorf("%w (pid %d): %v", ErrProcessMetadataUnsupported, pid, mapped)
		default:
			return "", fmt.Errorf("proc_pidpath for pid %d failed: %v", pid, mapped)
		}
	}

	if n < 0 {
		// proc_pidpath failed without a mapped errno class — fail closed.
		return "", fmt.Errorf("proc_pidpath for pid %d failed with no typed errno class", pid)
	}

	if n == 0 {
		// No resolvable image path: identity is NOT provable.
		return "", ErrProcessNotFound
	}

	end := int(n)
	if end > pathSize {
		end = pathSize
	}

	return string(buf[:end]), nil
}

// processStartTimeImpl on darwin: PROC_PIDTBSDINFO (proc_pidinfo) carries
// pbi_start_tvsec/pbi_start_tvusec — the absolute epoch start time of the
// process incarnation (run 44457547289 §1.3.a item 2: PID-reuse defense
// needs the real start identity, not a name). The answer is typed:
//   - a vanished PID is ErrProcessNotFound;
//   - an uninspectable PID is a typed denied error — callers see "unknown",
//     never a fabricated timestamp (run 44306343338 repair §4.7).
func processStartTimeImpl(pid int) (time.Time, error) {
	if pid <= 0 {
		return time.Time{}, ErrProcessNotFound
	}

	var (
		sec  C.longlong
		usec C.longlong
	)

	ret, err := C.c_start_time(C.int(pid), &sec, &usec)
	if err != nil {
		// Map the RAW kernel errno first — the typed sentinels are only
		// produced by mapProcInfoErrno, never by the cgo call itself.
		switch mapped := mapProcInfoErrno(err); {
		case errors.Is(mapped, errProcGone):
			return time.Time{}, ErrProcessNotFound
		case errors.Is(mapped, errProcAccessDenied):
			return time.Time{}, fmt.Errorf("%w (pid %d): %v", ErrProcessMetadataUnsupported, pid, mapped)
		default:
			return time.Time{}, fmt.Errorf("darwin start-time probe for pid %d failed: %v", pid, mapped)
		}
	}

	if ret != 0 {
		// A short PROC_PIDTBSDINFO answer carries NO start identity.
		return time.Time{}, fmt.Errorf("darwin start-time probe for pid %d returned %d of %d bytes", pid, ret, int(C.c_bsdinfo_size()))
	}

	return time.Unix(int64(sec), int64(usec)*1_000).UTC(), nil
}
