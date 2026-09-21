package dev.kerti.afloat.config

import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.shouldBe
import java.time.Duration

data class DurationParsingCase(
    val input: String,
    val expectedDuration: Duration
)

open class DurationParserSpec : StringSpec({

    withData(
        nameFn = { "Parses '${it.input}'" },
        listOf(
            DurationParsingCase("-1h30m", Duration.ofMinutes(-90)),
            DurationParsingCase("-1s", Duration.ofSeconds(-1)),
            DurationParsingCase("1500ns", Duration.ofNanos(1500)),
            DurationParsingCase("1500us", Duration.ofNanos(1500 * 1000)),
            DurationParsingCase("1500ms", Duration.ofMillis(1500)),
            DurationParsingCase("+1s", Duration.ofSeconds(1)),
            DurationParsingCase("30s", Duration.ofSeconds(30)),
            DurationParsingCase("1h30m", Duration.ofMinutes(90)),
            DurationParsingCase("1h29m60s", Duration.ofMinutes(90)),
            DurationParsingCase("1.5h", Duration.ofMinutes(90)),
            DurationParsingCase("720h", Duration.ofDays(30)),
            DurationParsingCase("2160h", Duration.ofDays(90)),

            // Go's accept-set, the corners of it. Each of these parses in
            // time.ParseDuration, so each must parse here: a value an operator
            // can set on one backend and not the other is the whole failure
            // BOOTSTRAP.md §12 exists to prevent, and it does not become less
            // true for being an unusual spelling.

            // The one unitless component Go allows, with and without a sign.
            DurationParsingCase("0", Duration.ZERO),
            DurationParsingCase("+0", Duration.ZERO),
            DurationParsingCase("-0", Duration.ZERO),

            // leadingInt and leadingFraction may each be empty, not both.
            DurationParsingCase(".5s", Duration.ofMillis(500)),
            DurationParsingCase("1.h", Duration.ofHours(1)),
            DurationParsingCase("1.5s", Duration.ofMillis(1500)),

            // Go spells microseconds three ways: us, µs (U+00B5) and μs
            // (U+03BC). Its own documentation uses the sign, so an operator who
            // copies from there lands on the two nobody thinks to test.
            DurationParsingCase("1500µs", Duration.ofNanos(1500 * 1000)),
            DurationParsingCase("1500μs", Duration.ofNanos(1500 * 1000)),
            DurationParsingCase("1h30m0µs", Duration.ofMinutes(90)),
        ),
    ) { (input, expectedDuration) ->
        val parsed = DurationParser.parse(input)
        parsed shouldBe expectedDuration
    }

    withData(
        nameFn = { "Rejects '$it'" },
        listOf(
            " 30d",
            "30d ",
            " 30d ",
            "30d",
            "",
            " ",
            "-",
            "abc",
            "10000000000000000000us",
            "2562047h48m",
            "1h-30m",
            "1h+30m",
            // Still rejected after the mantissa was widened to Go's: a unit
            // with no digits at all on either side of the point is not a
            // component, it is a typo.
            ".s",
            ".h",
            "-.s",
            ".",
            // The bare-zero special case is exactly that. Nothing else may go
            // without a unit.
            "00",
            "0.0",
            "1",
            "0d",
        )
    ) { input ->
        shouldThrow<IllegalArgumentException> { DurationParser.parse(input) }
    }
})
