package dev.kerti.afloat.config

import java.time.Duration
import kotlin.math.abs

object DurationParser {

    private const val OVERFLOW_LIMIT = Long.MAX_VALUE.toDouble()

    // The mantissa is Go's, not a tidier version of it: leadingInt followed by
    // an optional leadingFraction, where EITHER may be empty as long as one of
    // them is not. So `1.5h`, `.5s` and `1.h` all parse in Go, and a
    // `[0-9]+(\.[0-9]+)?` shape silently refuses the last two.
    //
    // `ms` precedes `s`, and `m` follows both, or `1ms` would match `m` and
    // leave a stray `s` for the reconstruction check to reject.
    private val durationPattern =
        "((?:[0-9]+(?:\\.[0-9]*)?)|(?:\\.[0-9]+))(ns|us|µs|μs|ms|s|m|h)".toRegex()

    /*
    Pure function to parse duration so the app can reach configuration parity
    with its Go counterpart. Accepts ns, us (and both micro signs), ms, s, m, h.
    Does not accept d (days). Allows fractional inputs (1.5h). Expects no
    whitespace.
     */
    fun parse(input: String): Duration {
        if (input.isEmpty()) {
            throw IllegalArgumentException("Duration string cannot be empty")
        }

        // Go's one special case: a bare zero needs no unit, with or without a
        // sign. Nothing else in the grammar allows a unitless component, so it
        // is handled here rather than bent into the pattern.
        if (input == "0" || input == "+0" || input == "-0") {
            return Duration.ZERO
        }

        val matches = durationPattern.findAll(input).toList()
        if (matches.isEmpty()) {
            throw IllegalArgumentException("Duration string cannot be empty")
        }

        val sign = if (input.substring(0, 1).matches("[+-]".toRegex())) {
            input.substring(0, 1)
        } else {
            ""
        }
        val signMultiplier = if (sign == "-") {
            -1
        } else {
            1
        }

        val reconstructed = sign.plus(matches.joinToString("") { it.value })
        if (input != reconstructed) {
            throw IllegalArgumentException("Invalid duration format or unsupported unit in: '$input'")
        }

        var totalNanosDouble = 0.0

        for (match in matches) {
            val (valueStr, unit) = match.destructured
            val value = valueStr.toDouble()

            val multiplier = when (unit) {
                "h" -> 3_600_000_000_000.0
                "m" -> 60_000_000_000.0
                "s" -> 1_000_000_000.0
                "ms" -> 1_000_000.0
                // Go spells microseconds three ways, and an operator who copies
                // a value out of Go's own docs gets one of the two it does not
                // occur to anyone to test.
                "us", "µs", "μs" -> 1_000.0
                "ns" -> 1.0
                else -> throw IllegalArgumentException("Unsupported unit '${unit}'")
            }

            val componentNanos = value * multiplier * signMultiplier

            if (abs(componentNanos) >= OVERFLOW_LIMIT) {
                throw IllegalArgumentException("Duration component will overflow: '$valueStr$unit' in '$input'")
            }

            if (abs(totalNanosDouble) + abs(componentNanos) >= OVERFLOW_LIMIT) {
                throw IllegalArgumentException("Duration string total will overflow: '$input'")
            }

            totalNanosDouble += componentNanos
        }

        return Duration.ofNanos(totalNanosDouble.toLong())
    }
}
