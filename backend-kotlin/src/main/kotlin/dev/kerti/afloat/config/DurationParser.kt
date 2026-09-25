package dev.kerti.afloat.config

import java.time.Duration

// Go's time.ParseDuration, ported line for line so the two backends read every
// duration variable identically (BOOTSTRAP.md §12): ns, us (and both micro
// signs), ms, s, m, h, never d; fractions (`1.5h`, `.5s`, `1.h`); a bare `0`
// with or without a sign; no whitespace.
//
// Unsigned 64-bit arithmetic throughout, as Go's is. An earlier version summed
// in a Double, which is exact only to 2^53 ns (about 104 days), so a value past
// that parsed to a different Duration than Go's (#32 item 5). The overflow
// bound is Go's too: a magnitude past 2^63 ns is refused, and exactly 2^63 is
// allowed only when negative.
object DurationParser {

    private val LIMIT: ULong = 1uL shl 63

    private val units: Map<String, ULong> = mapOf(
        "ns" to 1uL,
        "us" to 1_000uL,
        "µs" to 1_000uL, // U+00B5, the micro sign
        "μs" to 1_000uL, // U+03BC, Greek mu
        "ms" to 1_000_000uL,
        "s" to 1_000_000_000uL,
        "m" to 60_000_000_000uL,
        "h" to 3_600_000_000_000uL,
    )

    fun parse(input: String): Duration {
        var s = input
        var neg = false
        if (s.isNotEmpty() && (s[0] == '-' || s[0] == '+')) {
            neg = s[0] == '-'
            s = s.substring(1)
        }
        // Go's one special case: nothing else in the grammar allows a
        // component with no unit.
        if (s == "0") return Duration.ZERO
        if (s.isEmpty()) invalid(input)

        var d = 0uL
        while (s.isNotEmpty()) {
            if (!(s[0] == '.' || s[0] in '0'..'9')) invalid(input)

            val beforeInt = s.length
            val (v0, afterInt) = leadingInt(s) ?: overflow(input)
            var v = v0
            s = afterInt
            val pre = beforeInt != s.length

            var f = 0uL
            var scale = 1.0
            var post = false
            if (s.isNotEmpty() && s[0] == '.') {
                s = s.substring(1)
                val beforeFraction = s.length
                val fraction = leadingFraction(s)
                f = fraction.value
                scale = fraction.scale
                s = fraction.rest
                post = beforeFraction != s.length
            }
            // `.s` and `.` have neither half of a number.
            if (!pre && !post) invalid(input)

            var i = 0
            while (i < s.length && !(s[i] == '.' || s[i] in '0'..'9')) i++
            if (i == 0) throw IllegalArgumentException("Missing unit in duration '$input'")
            val u = s.substring(0, i)
            s = s.substring(i)
            val unit = units[u] ?: throw IllegalArgumentException("Unknown unit '$u' in duration '$input'")

            if (v > LIMIT / unit) overflow(input)
            v *= unit
            if (f > 0uL) {
                // float64(f) * (float64(unit) / scale), truncated, as in Go.
                v += (f.toDouble() * (unit.toDouble() / scale)).toULong()
                if (v > LIMIT) overflow(input)
            }
            d += v
            if (d > LIMIT) overflow(input)
        }

        if (neg) return Duration.ofNanos(-d.toLong())
        if (d > LIMIT - 1uL) overflow(input)
        return Duration.ofNanos(d.toLong())
    }

    // Go's leadingInt: null on overflow past 2^63.
    private fun leadingInt(s: String): Pair<ULong, String>? {
        var x = 0uL
        var i = 0
        while (i < s.length && s[i] in '0'..'9') {
            if (x > LIMIT / 10uL) return null
            x = x * 10uL + (s[i] - '0').toULong()
            if (x > LIMIT) return null
            i++
        }
        return x to s.substring(i)
    }

    private class Fraction(val value: ULong, val scale: Double, val rest: String)

    // Go's leadingFraction: digits past what fits are consumed and ignored,
    // never an error.
    private fun leadingFraction(s: String): Fraction {
        var x = 0uL
        var scale = 1.0
        var overflow = false
        var i = 0
        while (i < s.length && s[i] in '0'..'9') {
            if (!overflow) {
                if (x > (LIMIT - 1uL) / 10uL) {
                    overflow = true
                } else {
                    val y = x * 10uL + (s[i] - '0').toULong()
                    if (y > LIMIT) {
                        overflow = true
                    } else {
                        x = y
                        scale *= 10
                    }
                }
            }
            i++
        }
        return Fraction(x, scale, s.substring(i))
    }

    private fun invalid(input: String): Nothing =
        throw IllegalArgumentException("Invalid duration '$input'")

    private fun overflow(input: String): Nothing =
        throw IllegalArgumentException("Duration '$input' overflows")
}
