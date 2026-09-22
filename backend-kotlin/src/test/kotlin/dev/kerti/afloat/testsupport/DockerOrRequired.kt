package dev.kerti.afloat.testsupport

import io.kotest.core.annotation.Condition
import io.kotest.core.spec.Spec
import kotlin.reflect.KClass

// Mirrors Go's testutil/db.go require/skip contract: no Docker skips the spec,
// unless AFLOAT_REQUIRE_TEST_DB=1, in which case the spec stays enabled and
// fails loudly when it tries to start.
class DockerOrRequired : Condition {
    override fun evaluate(kclass: KClass<out Spec>): Boolean = TestDatabase.isEnabled()
}
