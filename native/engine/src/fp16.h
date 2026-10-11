// fp16.h — software IEEE 754 binary16 (half precision) conversion.
//
// The KV cache stores K/V activations as REAL 16-bit floats (uint16_t
// holding the bit pattern). This header provides the only sanctioned
// conversions between float (fp32) and fp16 bit patterns:
//
//   - fp32_to_fp16_bits: round-to-nearest-even, preserving Inf/NaN,
//     with correct gradual underflow into fp16 subnormals;
//   - fp16_bits_to_fp32: exact widening (every fp16 value is exactly
//     representable in fp32).
//
// The conversion is pure integer arithmetic — NO hardware __fp16/_Float16
// type is used, so the numerical engine stays platform-neutral (identical
// results on x86-64, ARM64, MSVC and GCC — bit-exact across platforms,
// which the reference tests rely on).
//
// Bit-exact against the IEEE 754 half-precision specification; the
// round-to-nearest-even tie rule matches F16C hardware behaviour
// (verified in test_kv_cache.cpp and the forward reference tests).

#ifndef SHTN_FP16_H
#define SHTN_FP16_H

#include <cstdint>
#include <cstring>

namespace shtn {
namespace fp16 {

// Convert one fp32 value to its fp16 bit pattern (round-to-nearest-even).
inline uint16_t fp32_to_fp16_bits(float value) {
    uint32_t bits = 0;
    std::memcpy(&bits, &value, sizeof(bits));

    const uint32_t sign = (bits >> 16) & 0x00008000u;
    const uint32_t biased_exp = (bits >> 23) & 0x000000FFu;
    const uint32_t mant = bits & 0x007FFFFFu;

    // fp32 zero / subnormal: every such magnitude is below half the fp16
    // minimum subnormal granularity in the RNE sense for true subnormals,
    // and a true fp32 zero maps to a signed fp16 zero.
    if (biased_exp == 0) {
        return static_cast<uint16_t>(sign);
    }

    // fp32 Inf / NaN.
    if (biased_exp == 0xFFu) {
        if (mant != 0) {
            // NaN: keep the top mantissa bits so it stays a NaN.
            return static_cast<uint16_t>(sign | 0x7E00u | (mant >> 13));
        }
        return static_cast<uint16_t>(sign | 0x7C00u);
    }

    const int32_t exp = static_cast<int32_t>(biased_exp) - 127;

    // Overflow: |value| >= 2^16 = 65536 is always beyond the fp16 max
    // (65504; the round-to-Inf threshold is 65520), so exp == 16 and up
    // saturate to Inf. exp == 15 values up to 65519.9 still round DOWN to
    // the max normal — the normal path below handles that, including the
    // carry-to-Inf at exactly 65520 (tie, rounds to even).
    if (exp > 15) {
        return static_cast<uint16_t>(sign | 0x7C00u);
    }

    if (exp >= -14) {
        // Normalized fp16: value = 1.mant * 2^exp.
        uint32_t m = mant >> 13;
        uint32_t e = static_cast<uint32_t>(exp + 15);

        // Round-to-nearest-even on the 13 dropped bits.
        const uint32_t dropped = mant & 0x1FFFu;
        const uint32_t half = 0x1000u;
        if (dropped > half || (dropped == half && (m & 1u) != 0)) {
            ++m;
            if (m == 0x0400u) {
                // Mantissa carry: renormalize (or overflow to Inf).
                m = 0;
                ++e;
                if (e >= 0x1Fu) {
                    return static_cast<uint16_t>(sign | 0x7C00u);
                }
            }
        }

        return static_cast<uint16_t>(sign | (e << 10) | m);
    }

    // Subnormal fp16 or underflow: value = sig * 2^(exp - 23) where
    // sig = 0x800000 | mant. The fp16 subnormal grid is m * 2^-24, so
    // the target mantissa is sig * 2^(exp + 1) — an integer shift right
    // for exp in [-25, -15]. shift == 24 (exp == -25) still rounds: a
    // value in (2^-25, 2^-24) is closer to 2^-24 than to 0 and must
    // round UP to the smallest subnormal (0x0001).
    const uint32_t sig = mant | 0x00800000u;
    const int32_t shift = -(exp + 1); // exp=-15 → 14, exp=-24 → 23, exp=-25 → 24

    if (shift > 24) {
        // |value| < 2^-25 + half its granularity: rounds to zero.
        return static_cast<uint16_t>(sign);
    }

    uint32_t m = sig >> shift;
    const uint32_t dropped = sig & ((1u << shift) - 1u);
    const uint32_t half = 1u << (shift - 1);

    if (dropped > half || (dropped == half && (m & 1u) != 0)) {
        ++m;
    }

    if (m >= 0x0400u) {
        // Rounded up into the normalized range: smallest normal fp16.
        return static_cast<uint16_t>(sign | (1u << 10));
    }

    return static_cast<uint16_t>(sign | m);
}

// Convert one fp16 bit pattern to fp32 (exact).
inline float fp16_bits_to_fp32(uint16_t bits) {
    const uint32_t sign = static_cast<uint32_t>(bits & 0x8000u) << 16;
    const uint32_t exp = (bits >> 10) & 0x001Fu;
    const uint32_t mant = bits & 0x03FFu;

    uint32_t out = 0;

    if (exp == 0) {
        if (mant == 0) {
            // Signed zero.
            out = sign;
        } else {
            // Subnormal fp16: value = mant * 2^-24. Normalize so the
            // leading 1 sits at bit 10, then the fp32 exponent is
            // 127 + (10 - 24 - shifts).
            uint32_t m = mant;
            uint32_t shifts = 0;
            while ((m & 0x0400u) == 0) {
                m <<= 1;
                ++shifts;
            }
            const uint32_t exp32 = 127u + 10u - 24u - shifts;
            out = sign | (exp32 << 23) | ((m & 0x03FFu) << 13);
        }
    } else if (exp == 0x1Fu) {
        // Inf / NaN.
        out = sign | 0x7F800000u | (mant << 13);
    } else {
        // Normal fp16.
        out = sign | ((exp + 127u - 15u) << 23) | (mant << 13);
    }

    float f = 0.0f;
    std::memcpy(&f, &out, sizeof(f));
    return f;
}

} // namespace fp16
} // namespace shtn

#endif /* SHTN_FP16_H */
