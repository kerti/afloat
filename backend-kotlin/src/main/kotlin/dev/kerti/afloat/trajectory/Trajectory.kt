package dev.kerti.afloat.trajectory

import java.math.BigDecimal

// The daily-bucket trajectory figures (docs/CONTEXT.md §Calculation spec).
// Pure: no Spring, no database, no dates - callers pass aggregates, and
// contract/testdata/trajectory.json holds the answers both backends must give.
//
// Rulings R1-R4 on issue #73 govern every number here. Compute exactly and
// round only on output: allowance figures (B, A, Left, Forward Daily Rate)
// floor toward negative infinity at scale 4 (RoundingMode.FLOOR, not DOWN);
// Trajectory Variance and Buffer Days round HALF_UP at scale 4; State compares
// V against threshold x B exactly, never a rounded or divided value. Left to
// Spend Today is the one figure built from a rounded one: floored A - S_today.

enum class Mode(val wire: String) {
    ADAPTIVE("adaptive"),
    FIXED("fixed"),
}

enum class State(val wire: String) {
    AFLOAT("afloat"),
    DRIFTING("drifting"),
    TAKING_ON_WATER("taking_on_water"),
}

// Configuration, not constants (ruling R3; [OPEN Q-05] stays open).
// State is pinned to AFLOAT while daysElapsed < warmUpDays. The thresholds are
// multiples of B: V >= driftingBelow x B is AFLOAT, V >= takingOnWaterBelow x B
// is DRIFTING, otherwise TAKING_ON_WATER.
data class Parameters(
    val warmUpDays: Int,
    val driftingBelow: BigDecimal,
    val takingOnWaterBelow: BigDecimal,
) {
    companion object {
        // Mirrors the fixture's top-level `parameters`, as Go's DefaultParameters does.
        val DEFAULT = Parameters(warmUpDays = 3, driftingBelow = BigDecimal("-1"), takingOnWaterBelow = BigDecimal("-3"))
    }
}

data class TrajectoryInputs(
    val dailyPool: BigDecimal, // P
    val periodDays: Int, // D
    val daysElapsed: Int, // e: settled days, strictly before Today
    val settledSpend: BigDecimal, // S_settled
    val todaySpend: BigDecimal, // S_today
    val futureSpend: BigDecimal, // S_future
    val mode: Mode,
    val parameters: Parameters = Parameters.DEFAULT,
)

// Every BigDecimal at scale 4: the scale is part of the contract (BOOTSTRAP.md §4).
data class Figures(
    val baselineDailyBudget: BigDecimal,
    val availablePool: BigDecimal,
    val todaysAllowance: BigDecimal,
    val leftToSpendToday: BigDecimal,
    val forwardDailyRate: BigDecimal?, // null on the last day (r = 1)
    val trajectoryVariance: BigDecimal,
    val bufferDays: BigDecimal,
    val state: State,
)

data class TargetLineInputs(
    val periodDays: Int, // D
    val dailyPoolBefore: BigDecimal,
    val dailyPoolAfter: BigDecimal, // effective from effectiveDay
    val effectiveDay: Int, // j, 1-indexed
)

// The cumulative target line across a mid-period Budget change
// (docs/CONTEXT.md §Mid-period Budget changes).
interface TargetLine {
    // B_new floored at scale 4. May be zero or negative; that is what the
    // clamp responds to, not an error.
    val baselineAfter: BigDecimal

    // Cumulative target through a 1-indexed day, HALF_UP at scale 4; never
    // below T(j-1) from effectiveDay on.
    fun at(day: Int): BigDecimal
}

object Trajectory {
    // Throws IllegalArgumentException, as Go's Calculate returns an error, for
    // periodDays <= 0, daysElapsed >= periodDays, and a zero dailyPool.
    fun calculate(inputs: TrajectoryInputs): Figures = TODO("#74: Rad writes the calculation")

    // Throws IllegalArgumentException for periodDays <= 0 or an effectiveDay
    // outside 1..periodDays.
    fun targetLine(inputs: TargetLineInputs): TargetLine = TODO("#74: Rad writes the calculation")
}
