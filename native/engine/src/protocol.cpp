// protocol.cpp — frame implementation.

#include "protocol.h"

#include <array>
#include <cstring>

namespace shtn {
namespace protocol {

FrameStatus read_frame(std::istream& in, std::string& out) {
    out.clear();

    std::array<char, 4> header{};

    in.read(header.data(), static_cast<std::streamsize>(header.size()));

    if (in.gcount() == 0 && in.eof()) {
        return FrameStatus::Eof;
    }

    if (in.gcount() != static_cast<std::streamsize>(header.size())) {
        return FrameStatus::Truncated;
    }

    uint32_t size = 0;
    // Explicit little-endian decode (portable regardless of host order):
    size = static_cast<uint32_t>(static_cast<unsigned char>(header[0])) |
           (static_cast<uint32_t>(static_cast<unsigned char>(header[1])) << 8) |
           (static_cast<uint32_t>(static_cast<unsigned char>(header[2])) << 16) |
           (static_cast<uint32_t>(static_cast<unsigned char>(header[3])) << 24);

    if (size == 0) {
        return FrameStatus::Invalid;
    }

    if (size > kMaxFrameBytes) {
        return FrameStatus::TooLarge;
    }

    out.resize(size);

    in.read(out.data(), static_cast<std::streamsize>(size));

    if (in.gcount() != static_cast<std::streamsize>(size)) {
        out.clear();
        return FrameStatus::Truncated;
    }

    return FrameStatus::Ok;
}

bool write_frame(std::ostream& out, const std::string& payload) {
    if (payload.size() > kMaxFrameBytes) {
        return false;
    }

    const uint32_t size = static_cast<uint32_t>(payload.size());

    std::array<unsigned char, 4> header{
        static_cast<unsigned char>(size & 0xFFu),
        static_cast<unsigned char>((size >> 8) & 0xFFu),
        static_cast<unsigned char>((size >> 16) & 0xFFu),
        static_cast<unsigned char>((size >> 24) & 0xFFu),
    };

    out.write(reinterpret_cast<const char*>(header.data()),
              static_cast<std::streamsize>(header.size()));

    out.write(payload.data(), static_cast<std::streamsize>(payload.size()));

    out.flush();

    return static_cast<bool>(out);
}

} // namespace protocol
} // namespace shtn
