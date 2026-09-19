package dev.kerti.afloat.system

import dev.kerti.afloat.api.model.Health
import dev.kerti.afloat.config.AppConfig
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import org.springframework.http.HttpStatus

open class SystemControllerSpec : StringSpec({

    // A version pinned beyond the default so the flow from config into the
    // response is asserted, not assumed.
    val config = AppConfig.load(
        mapOf("DATABASE_URL" to "postgres://localhost/afloat_test", "VERSION" to "1.2.3")
    )

    "reports ok when the database is reachable" {
        val controller = SystemController(DatabaseProbe { true }, config)

        val response = controller.getHealth()

        response.statusCode shouldBe HttpStatus.OK
        response.body shouldBe Health(Health.Status.ok, "1.2.3")
    }

    "reports degraded with 503 when the database is unreachable" {
        val controller = SystemController(DatabaseProbe { false }, config)

        val response = controller.getHealth()

        // A database that is down is a reportable state, not an error: the probe
        // consumes a body, not an exception. Mirrors Go's system_test.go.
        response.statusCode shouldBe HttpStatus.SERVICE_UNAVAILABLE
        response.body shouldBe Health(Health.Status.degraded, "1.2.3")
    }
})