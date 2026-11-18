//go:build darwin && cgo

package resources

/*
#include <mach/mach.h>
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

// procRAMImpl (darwin, cgo build): the resident set of the CURRENT
// process through the Mach task self-port — task_info(TASK_VM_INFO)
// resident_size, the documented native process-memory API. Other PIDs
// require the libproc proc_pidinfo fd walk, which this build does not
// link: those return the typed ErrProcRAMUnsupported instead of a
// fabricated value (work order §4.6).
func procRAMImpl(pid int) (int64, error) {
	if pid != os.Getpid() {
		return 0, ErrProcRAMUnsupported
	}

	var vm C.task_vm_info_data_t
	var count C.mach_msg_type_number_t = C.TASK_VM_INFO_COUNT

	kr := C.task_info(C.mach_task_self_, C.TASK_VM_INFO, C.task_info_t(unsafe.Pointer(&vm)), &count)
	if kr != C.KERN_SUCCESS {
		return 0, fmt.Errorf("task_info(TASK_VM_INFO) failed: kern_return_t=%d", int(kr))
	}

	return int64(vm.resident_size), nil
}
