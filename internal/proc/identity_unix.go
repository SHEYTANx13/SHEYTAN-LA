//go:build !windows

package proc

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listeningProcessImpl resolves the PID owning a listening TCP socket on
// Linux through /proc/net/tcp{,6} + /proc/<pid>/fd socket-inode matching.
// It compiles on every Unix; on non-Linux Unixes /proc may not exist and
// the implementation reports ErrPortOwnerUnknown (fail closed).
func listeningProcessImpl(port int) (int, string, bool, error) {
	inodes, err := listeningInodes(port)
	if err != nil {
		return 0, "", false, err
	}

	if len(inodes) == 0 {
		return 0, "", false, nil
	}

	pid, err := pidForSocketInode(inodes)
	if err != nil {
		return 0, "", true, err
	}

	exe, err := processExePathImpl(pid)
	if err != nil {
		return pid, "", true, err
	}

	return pid, exe, true, nil
}

// listeningInodes returns the socket inodes of all IPv4/IPv6 listening
// sockets with the given local port, from /proc/net/tcp and tcp6.
func listeningInodes(port int) ([]string, error) {
	var inodes []string

	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(table)
		if err != nil {
			continue // no IPv6, or /proc not mounted
		}

		scanner := bufio.NewScanner(f)
		first := true

		for scanner.Scan() {
			line := scanner.Text()

			if first {
				first = false
				continue
			}

			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}

			// fields: sl local_address rem_address st ...
			local := strings.Split(fields[1], ":")
			if len(local) != 2 {
				continue
			}

			lport, err := strconv.ParseInt(local[1], 16, 32)
			if err != nil || int(lport) != port {
				continue
			}

			if fields[3] != "0A" { // TCP_LISTEN
				continue
			}

			// The inode is the last field of the socket tuple section.
			// Column 9 holds the inode in modern kernels.
			inode := fields[9]
			if inode != "" && inode != "0" {
				inodes = append(inodes, inode)
			}
		}

		_ = f.Close()
	}

	return inodes, nil
}

// pidForSocketInode scans /proc/<pid>/fd/* for a socket whose link target
// references one of the given inodes.
func pidForSocketInode(inodes []string) (int, error) {
	want := make(map[string]bool, len(inodes))
	for _, in := range inodes {
		want["socket:["+in+"]"] = true
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("%w: /proc unreadable: %v", ErrPortOwnerUnknown, err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}

		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // permission denied etc. — skip quietly
		}

		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}

			if want[target] {
				return pid, nil
			}
		}
	}

	return 0, fmt.Errorf("%w: listener inode not attributed to any readable /proc entry", ErrPortOwnerUnknown)
}

func processExePathImpl(pid int) (string, error) {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		// /proc/<pid>/exe can be unreadable for foreign users; fall back
		// to the cmdline argv[0] which is world-readable.
		if data, cerr := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); cerr == nil {
			if argv0 := strings.SplitN(string(data), "\x00", 2)[0]; argv0 != "" {
				return argv0, nil
			}
		}

		return "", fmt.Errorf("%w (pid %d): %v", ErrProcessNotFound, pid, err)
	}

	return exe, nil
}

func sameExecutableImpl(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}

	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}

	return a == b
}
