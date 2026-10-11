// util.h — small shared helpers for the engine core (internal, not ABI).

#ifndef SHTN_UTIL_H
#define SHTN_UTIL_H

#include <cstddef>
#include <cstring>
#include <string>

namespace shtn {

// copy_cstr copies src into the fixed-size dst buffer, always
// NUL-terminated, truncating safely when src does not fit.
inline void copy_cstr(char* dst, size_t cap, const char* src) {
    if (dst == nullptr || cap == 0) {
        return;
    }
    if (src == nullptr) {
        dst[0] = '\0';
        return;
    }
    const size_t n = std::strlen(src);
    const size_t m = n < cap - 1 ? n : cap - 1;
    std::memcpy(dst, src, m);
    dst[m] = '\0';
}

inline void copy_cstr(char* dst, size_t cap, const std::string& src) {
    copy_cstr(dst, cap, src.c_str());
}

} // namespace shtn

#endif /* SHTN_UTIL_H */
