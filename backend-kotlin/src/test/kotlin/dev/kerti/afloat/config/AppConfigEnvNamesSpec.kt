package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.TestConfigDefaults
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe

open class AppConfigEnvNamesSpec : StringSpec({

    "ENV_NAMES match the placeholders in application.yaml" {
        // Whole file, not the afloat: subtree: PORT lives under server:.
        val yaml = requireNotNull(javaClass.getResource("/application.yaml")).readText()
        val scraped = Regex("""\$\{([A-Z0-9_]+)""")
            .findAll(yaml)
            .map { it.groupValues[1] }
            .toSet()
        AppConfig.ENV_NAMES.toSet() shouldBe scraped
    }

    // The review finding this pins: application.yaml is the real, bound source of
    // defaults. If a default drifts from §12, check-env-parity.sh fails that on
    // its own; if the runtime reads something else - a missing afloat.* leaf, a
    // placeholder that went stale here, a default parse() would reject - this dies.
    "the resolved environment yields the yaml defaults field by field" {
        val cfg = AppConfig.from(
            TestConfigDefaults.environment("DATABASE_URL" to "postgres://host/afloat_kotlin")
        )
        val defaults = TestConfigDefaults.scrapedDefaults()

        cfg.databaseUrl shouldBe "postgres://host/afloat_kotlin"
        cfg.port shouldBe defaults.getValue("PORT").toInt()
        cfg.logFormat shouldBe defaults.getValue("LOG_FORMAT")
        cfg.logLevel shouldBe defaults.getValue("LOG_LEVEL")
        cfg.autoMigrate shouldBe AppConfig.parseBool("AUTO_MIGRATE", defaults.getValue("AUTO_MIGRATE"))
        cfg.readTimeout shouldBe DurationParser.parse(defaults.getValue("HTTP_READ_TIMEOUT"))
        cfg.writeTimeout shouldBe DurationParser.parse(defaults.getValue("HTTP_WRITE_TIMEOUT"))
        cfg.idleTimeout shouldBe DurationParser.parse(defaults.getValue("HTTP_IDLE_TIMEOUT"))
        cfg.shutdownTimeout shouldBe DurationParser.parse(defaults.getValue("SHUTDOWN_TIMEOUT"))
        cfg.authLocalEnabled shouldBe AppConfig.parseBool("AUTH_LOCAL_ENABLED", defaults.getValue("AUTH_LOCAL_ENABLED"))
        cfg.authGoogleEnabled shouldBe AppConfig.parseBool("AUTH_GOOGLE_ENABLED", defaults.getValue("AUTH_GOOGLE_ENABLED"))
        cfg.sessionTtl shouldBe DurationParser.parse(defaults.getValue("SESSION_TTL"))
        cfg.sessionMaxLifetime shouldBe DurationParser.parse(defaults.getValue("SESSION_MAX_LIFETIME"))
        cfg.cookieSecure shouldBe AppConfig.parseBool("COOKIE_SECURE", defaults.getValue("COOKIE_SECURE"))
        cfg.version shouldBe defaults.getValue("VERSION")
    }

    // A second default that drifts past parse() is caught here: from() and
    // parse() must agree on the same defaults, so a leaf read the wrong way
    // shows up as a mismatch.
    "from(Environment) and parse(values) agree on the yaml defaults" {
        val viaEnvironment = AppConfig.from(
            TestConfigDefaults.environment("DATABASE_URL" to "postgres://host/afloat_kotlin")
        )
        val viaValues = AppConfig.parse(TestConfigDefaults.testConfigBase())

        viaEnvironment shouldBe viaValues
    }
})
