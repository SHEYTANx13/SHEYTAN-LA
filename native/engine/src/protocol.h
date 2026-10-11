// protocol.h — frame reading/writing for the SHEYTAN Native API.
//
// Frame layout (identical to the Go side, internal/native/engine/protocol.go):
//
//   [4 bytes little-endian payload length][length bytes of UTF-8 JSON]
//
// Bounds: a frame larger than SHTN_MAX_FRAME_BYTES is a protocol
// violation. Short reads/writes are errors. EOF closes the stream
// cleanly (the host exits 0).

#ifndef SHTN_PROTOCOL_H
#define SHTN_PROTOCOL_H

#include <cstdint>
#include <istream>
#include <ostream>
#include <string>

namespace shtn {
namespace protocol {

constexpr uint32_t kMaxFrameBytes = 1u << 20; // 1 MiB cap

enum class FrameStatus {
    Ok,
    Eof,       // clean stream close
    TooLarge,  // frame exceeds the cap
    Truncated, // short read mid-frame
    Invalid,   // zero-length frame or header failure
};

// Read one frame. On Ok, out holds the payload bytes.
FrameStatus read_frame(std::istream& in, std::string& out);

// Write one frame.
bool write_frame(std::ostream& out, const std::string& payload);

} // namespace protocol
} // namespace shtn

#endif /* SHTN_PROTOCOL_H */
