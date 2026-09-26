package dev.kerti.afloat.trajectory

import io.kotest.assertions.assertSoftly
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.assertions.withClue
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.collections.shouldNotBeEmpty
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.nulls.shouldNotBeNull
import io.kotest.matchers.shouldBe
import tools.jackson.databind.JsonNode
import tools.jackson.databind.json.JsonMapper
import java.io.File
import java.math.BigDecimal

// The trajectory figures both backends must compute alike
// (contract/testdata/trajectory.json, issues #73 and #74). Go's
// fixture_test.go reads the same file. Every figure is compared as its
// scale-4 plain string, not with compareTo: "203448.2758" and
// "203448.27580" are equal numbers but different wire values, and the scale
// is part of the contract (BOOTSTRAP.md §4).
class TrajectoryFixtureSpec : StringSpec({

    val fixture = JsonMapper().readTree(File(System.getProperty("afloat.contract.testdata"), "trajectory.json"))

    fun JsonNode.text(field: String): String = get(field).asString()

    fun BigDecimal.wire(): String = toPlainString()

    fun JsonNode.parameters(defaults: Parameters): Parameters = Parameters(
        warmUpDays = get("warm_up_days")?.asInt() ?: defaults.warmUpDays,
        driftingBelow = get("drifting_below")?.let { BigDecimal(it.asString()) } ?: defaults.driftingBelow,
        takingOnWaterBelow = get("taking_on_water_below")?.let { BigDecimal(it.asString()) } ?: defaults.takingOnWaterBelow,
    )

    val defaults = fixture.get("parameters").parameters(Parameters.DEFAULT)

    "reads a non-empty fixture" {
        // A fixture that asserts nothing looks like coverage.
        fixture.get("figures").toList().shouldNotBeEmpty()
        fixture.get("target_line").toList().shouldNotBeEmpty()
    }

    "Parameters.DEFAULT is the fixture's top-level parameters" {
        Parameters.DEFAULT.warmUpDays shouldBe defaults.warmUpDays
        Parameters.DEFAULT.driftingBelow.compareTo(defaults.driftingBelow) shouldBe 0
        Parameters.DEFAULT.takingOnWaterBelow.compareTo(defaults.takingOnWaterBelow) shouldBe 0
    }

    withData(
        nameFn = { "figures: ${it.text("name")}" },
        fixture.get("figures").toList(),
    ) { row ->
        val given = row.get("inputs")
        val want = row.get("expected")
        val got = Trajectory.calculate(
            TrajectoryInputs(
                dailyPool = BigDecimal(given.text("daily_pool")),
                periodDays = given.get("period_days").asInt(),
                daysElapsed = given.get("days_elapsed").asInt(),
                settledSpend = BigDecimal(given.text("settled_spend")),
                todaySpend = BigDecimal(given.text("today_spend")),
                futureSpend = BigDecimal(given.text("future_spend")),
                mode = Mode.entries.single { it.wire == given.text("mode") },
                parameters = given.parameters(defaults),
            ),
        )
        assertSoftly {
            withClue("baseline_daily_budget") { got.baselineDailyBudget.wire() shouldBe want.text("baseline_daily_budget") }
            withClue("available_pool") { got.availablePool.wire() shouldBe want.text("available_pool") }
            withClue("todays_allowance") { got.todaysAllowance.wire() shouldBe want.text("todays_allowance") }
            withClue("left_to_spend_today") { got.leftToSpendToday.wire() shouldBe want.text("left_to_spend_today") }
            withClue("forward_daily_rate") {
                val forward = want.get("forward_daily_rate")
                if (forward.isNull) {
                    got.forwardDailyRate.shouldBeNull()
                } else {
                    got.forwardDailyRate.shouldNotBeNull().wire() shouldBe forward.asString()
                }
            }
            withClue("trajectory_variance") { got.trajectoryVariance.wire() shouldBe want.text("trajectory_variance") }
            withClue("buffer_days") { got.bufferDays.wire() shouldBe want.text("buffer_days") }
            withClue("state") { got.state.wire shouldBe want.text("state") }
        }
    }

    withData(
        nameFn = { "target line: ${it.text("name")}" },
        fixture.get("target_line").toList(),
    ) { row ->
        val given = row.get("inputs")
        val want = row.get("expected")
        val line = Trajectory.targetLine(
            TargetLineInputs(
                periodDays = given.get("period_days").asInt(),
                dailyPoolBefore = BigDecimal(given.text("daily_pool_before")),
                dailyPoolAfter = BigDecimal(given.text("daily_pool_after")),
                effectiveDay = given.get("effective_day").asInt(),
            ),
        )
        val targets = want.get("targets").toList()
        targets.shouldNotBeEmpty()
        assertSoftly {
            withClue("baseline_after") { line.baselineAfter.wire() shouldBe want.text("baseline_after") }
            targets.forEach { target ->
                val day = target.get("day").asInt()
                withClue("target at day $day") { line.at(day).wire() shouldBe target.text("target") }
            }
        }
    }

    // The inputs Go's Calculate and NewTargetLine refuse with an error
    // (errors_test.go). None is a fixture row: a row describes a valid read.
    val valid = TrajectoryInputs(
        dailyPool = BigDecimal("100.0000"),
        periodDays = 10,
        daysElapsed = 0,
        settledSpend = BigDecimal("0.0000"),
        todaySpend = BigDecimal("0.0000"),
        futureSpend = BigDecimal("0.0000"),
        mode = Mode.ADAPTIVE,
    )

    withData(
        nameFn = { "calculate refuses ${it.first}" },
        "non-positive period days" to valid.copy(periodDays = 0),
        "days elapsed at the end of the period (r = 0)" to valid.copy(daysElapsed = 10),
        "days elapsed past the end of the period" to valid.copy(daysElapsed = 11),
        "a zero daily pool (B = 0, so Buffer Days is undefined)" to valid.copy(dailyPool = BigDecimal("0.0000")),
    ) { (_, inputs) ->
        shouldThrow<IllegalArgumentException> { Trajectory.calculate(inputs) }
    }

    val validLine = TargetLineInputs(
        periodDays = 30,
        dailyPoolBefore = BigDecimal("100.0000"),
        dailyPoolAfter = BigDecimal("200.0000"),
        effectiveDay = 11,
    )

    withData(
        nameFn = { "targetLine refuses ${it.first}" },
        "non-positive period days" to validLine.copy(periodDays = 0),
        "effective day 0" to validLine.copy(effectiveDay = 0),
        "a negative effective day" to validLine.copy(effectiveDay = -1),
        "an effective day past the period" to validLine.copy(effectiveDay = 31),
    ) { (_, inputs) ->
        shouldThrow<IllegalArgumentException> { Trajectory.targetLine(inputs) }
    }
})
