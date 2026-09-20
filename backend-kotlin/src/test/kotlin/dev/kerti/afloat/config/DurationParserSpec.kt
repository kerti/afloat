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
            "1h-30m"
        )
    ) { input ->
        shouldThrow<IllegalArgumentException> { DurationParser.parse(input) }
    }
})
