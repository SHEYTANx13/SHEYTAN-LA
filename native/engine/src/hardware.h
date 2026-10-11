// hardware.h — platform hardware detection for the native engine.
//
// Detection rules (see types.h): real values or zeros — nothing is
// guessed, no vendor is hardcoded. Platform support:
//   Linux:   /proc/cpuinfo + sysconf + /proc/meminfo + /proc/self/statm
//   Windows: cpuid brand, GetLogicalProcessorInformation,
//            GlobalMemoryStatusEx, GetProcessMemoryInfo
//   macOS:   sysctl brand + memsize, mach task info for RSS

#ifndef SHTN_HARDWARE_H
#define SHTN_HARDWARE_H

#include "shtn/types.h"

namespace shtn {

// Detect CPU facts (name/cores/frequency). Unknown values stay zero.
void detect_cpu(shtn_cpu_info& out);

// Detect RAM facts. Unknown values stay zero.
void detect_ram(shtn_ram_info& out);

// Compile-time architecture string ("x86_64", "aarch64", ...).
const char* architecture_name();

// Measured resident set size of THIS process, in bytes (0 on failure).
uint64_t current_process_rss();

} // namespace shtn

#endif /* SHTN_HARDWARE_H */
