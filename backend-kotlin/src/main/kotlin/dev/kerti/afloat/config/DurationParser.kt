package dev.kerti.afloat.config

import java.time.Duration
import kotlin.math.abs

object DurationParser {

    private const val OVERFLOW_LIMIT = Long.MAX_VALUE.toDouble()
    private val durationPattern = "([0-9]+(?:\\.[0-9]+)?)(ns|us|ms|s|m|h)".toRegex()

    /*
    Pure function to parse duration so the app can reach configuration parity
    with its Go counterpart. Accepts ns, us, ms, s, m, h. Does not accept
    d (days). Allows fractional inputs (1.5h). Expects no whitespace.
     */
    fun parse(input: String): Duration {
        if (input.isEmpty()) {
            throw IllegalArgumentException("Duration string cannot be empty")
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
                "us" -> 1_000.0
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
