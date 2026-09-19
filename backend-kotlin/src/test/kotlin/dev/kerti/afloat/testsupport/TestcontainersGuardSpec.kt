package dev.kerti.afloat.testsupport

import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe

class TestcontainersGuardSpec : StringSpec({

    "a live Docker daemon proceeds whatever the require flag says" {
        TestDatabase.decision(available = true, requireTestDb = null) shouldBe TestDbDecision.PROCEED
        TestDatabase.decision(available = true, requireTestDb = "0") shouldBe TestDbDecision.PROCEED
        TestDatabase.decision(available = true, requireTestDb = "1") shouldBe TestDbDecision.PROCEED
    }

    "testcontainersFailsRatherThanSkipsWhenRequireTestDbIsSet" {
        TestDatabase.decision(available = false, requireTestDb = "1") shouldBe TestDbDecision.FAIL
    }

    "a missing daemon skips when the require flag is unset" {
        TestDatabase.decision(available = false, requireTestDb = null) shouldBe TestDbDecision.SKIP
    }

    "a missing daemon skips unless the require flag is exactly 1" {
        TestDatabase.decision(available = false, requireTestDb = "0") shouldBe TestDbDecision.SKIP
        TestDatabase.decision(available = false, requireTestDb = "") shouldBe TestDbDecision.SKIP
    }
})
