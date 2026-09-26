package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.TestConfigDefaults
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.shouldBe
import tools.jackson.databind.json.JsonMapper
import java.io.File
import java.time.Duration

data class AppConfigCase(
    val name: String,
    val input: Map<String, String>,
    val expectedConfig: AppConfig? = null,
    val expectedMessage: String? = null,
)

open class AppConfigSpec : StringSpec({

    withData(
        nameFn = { it.name },
        listOf(
            AppConfigCase(
                name = "Overrides and tolerant bools", input = TestConfigDefaults.testConfigBase(
                    "DATABASE_URL" to "garbage",
                    "PORT" to "8080",
                    "LOG_FORMAT" to "json",
                    "LOG_LEVEL" to "debug",
                    "AUTO_MIGRATE" to "1",
                    "AUTH_LOCAL_ENABLED" to "0",
                    "AUTH_GOOGLE_ENABLED" to "TRUE",
                    "COOKIE_SECURE" to "f",
                    "HTTP_READ_TIMEOUT" to "45s",
                    "SESSION_TTL" to "48h",
                    "SESSION_MAX_LIFETIME" to "96h",
                    "VERSION" to "1.2.3"
                ), expectedConfig = AppConfig(
                    databaseUrl = "garbage",
                    port = 8080,
                    logFormat = "json",
                    logLevel = "debug",
                    autoMigrate = true,
                    readTimeout = Duration.ofSeconds(45),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = false,
                    authGoogleEnabled = true,
                    sessionTtl = Duration.ofHours(48),
                    sessionMaxLifetime = Duration.ofHours(96),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = false,
                    version = "1.2.3",
                )
            ),
            AppConfigCase(
                name = "Blank VERSION falls back to 'dev'",
                input = TestConfigDefaults.testConfigBase("VERSION" to " "),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 5183,
                    logFormat = "text",
                    logLevel = "info",
                    autoMigrate = true,
                    readTimeout = Duration.ofSeconds(30),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = true,
                    authGoogleEnabled = false,
                    sessionTtl = Duration.ofHours(720),
                    sessionMaxLifetime = Duration.ofHours(2160),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = true,
                    version = "dev",
                )
            ),
            AppConfigCase(
                name = "HTTP_READ_TIMEOUT: '1m30s' — compound, Go-valid",
                input = TestConfigDefaults.testConfigBase("HTTP_READ_TIMEOUT" to "1m30s"),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 5183, logFormat = "text", logLevel = "info", autoMigrate = true,
                    readTimeout = Duration.ofMinutes(1).plusSeconds(30),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = true, authGoogleEnabled = false,
                    sessionTtl = Duration.ofHours(720),
                    sessionMaxLifetime = Duration.ofHours(2160),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = true, version = "dev",
                )
            ),
            AppConfigCase(
                name = "HTTP_READ_TIMEOUT: '1.5h' — fractional, Go-valid",
                input = TestConfigDefaults.testConfigBase("HTTP_READ_TIMEOUT" to "1.5h"),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 5183, logFormat = "text", logLevel = "info", autoMigrate = true,
                    readTimeout = Duration.ofMinutes(90),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = true, authGoogleEnabled = false,
                    sessionTtl = Duration.ofHours(720),
                    sessionMaxLifetime = Duration.ofHours(2160),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = true, version = "dev",
                )
            ),
            // #69: a duration variable set to the empty string is unset, not a
            // parse failure - "FOO=" in a .env boots with FOO's documented
            // default, matching Go's caarlos0/env. Every duration variable, not
            // only the login backoffs this issue started from.
            AppConfigCase(
                name = "every duration variable, blank, falls back to its default",
                input = TestConfigDefaults.testConfigBase(
                    "HTTP_READ_TIMEOUT" to "",
                    "HTTP_WRITE_TIMEOUT" to "",
                    "HTTP_IDLE_TIMEOUT" to "",
                    "SHUTDOWN_TIMEOUT" to "",
                    "SESSION_TTL" to "",
                    "SESSION_MAX_LIFETIME" to "",
                    "LOGIN_FIRST_BACKOFF" to "",
                    "LOGIN_MAX_BACKOFF" to "",
                ),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 5183, logFormat = "text", logLevel = "info", autoMigrate = true,
                    readTimeout = Duration.ofSeconds(30),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = true, authGoogleEnabled = false,
                    sessionTtl = Duration.ofHours(720),
                    sessionMaxLifetime = Duration.ofHours(2160),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = true, version = "dev",
                )
            ),
            AppConfigCase(
                name = "SESSION_TTL: '36h30m' — compound, Go-valid",
                input = TestConfigDefaults.testConfigBase(
                    "SESSION_TTL" to "36h30m",
                    "SESSION_MAX_LIFETIME" to "96h",
                ),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 5183, logFormat = "text", logLevel = "info", autoMigrate = true,
                    readTimeout = Duration.ofSeconds(30),
                    writeTimeout = Duration.ofSeconds(60),
                    idleTimeout = Duration.ofSeconds(120),
                    shutdownTimeout = Duration.ofSeconds(10),
                    authLocalEnabled = true, authGoogleEnabled = false,
                    sessionTtl = Duration.ofMinutes(36 * 60 + 30),
                    sessionMaxLifetime = Duration.ofHours(96),
                    loginFirstBackoff = Duration.ofSeconds(1),
                    loginMaxBackoff = Duration.ofMinutes(5),
                    cookieSecure = true, version = "dev",
                )
            ),
        ),
    ) { (_, input, expectedConfig) ->
        val cfg = AppConfig.parse(input)
        cfg shouldBe expectedConfig
    }

    withData(
        nameFn = { it.name }, listOf(
            AppConfigCase(
                name = "DATABASE_URL: ''",
                input = TestConfigDefaults.testConfigBase("DATABASE_URL" to ""),
                expectedMessage = "DATABASE_URL is empty",
            ),
            AppConfigCase(
                name = "DATABASE_URL: '    '",
                input = TestConfigDefaults.testConfigBase("DATABASE_URL" to "    "),
                expectedMessage = "DATABASE_URL is empty",
            ),
            AppConfigCase(
                name = "PORT: '0'",
                input = TestConfigDefaults.testConfigBase("PORT" to "0"),
                expectedMessage = "PORT '0' is not a valid port",
            ),
            AppConfigCase(
                name = "PORT: '-1'",
                input = TestConfigDefaults.testConfigBase("PORT" to "-1"),
                expectedMessage = "PORT '-1' is not a valid port",
            ),
            AppConfigCase(
                name = "PORT: '70000'",
                input = TestConfigDefaults.testConfigBase("PORT" to "70000"),
                expectedMessage = "PORT '70000' is not a valid port",
            ),
            AppConfigCase(
                name = "PORT: 'abc'",
                input = TestConfigDefaults.testConfigBase("PORT" to "abc"),
                expectedMessage = "PORT: 'abc' is not a valid integer",
            ),
            AppConfigCase(
                name = "PORT missing from the source",
                input = TestConfigDefaults.testConfigBase() - "PORT",
                expectedMessage = "PORT is not set in application.yaml",
            ),
            AppConfigCase(
                name = "LOG_FORMAT: 'yaml'",
                input = TestConfigDefaults.testConfigBase("LOG_FORMAT" to "yaml"),
                expectedMessage = "LOG_FORMAT 'yaml': want text or json",
            ),
            AppConfigCase(
                name = "LOG_LEVEL: 'verbose'",
                input = TestConfigDefaults.testConfigBase("LOG_LEVEL" to "verbose"),
                expectedMessage = "LOG_LEVEL 'verbose': want debug, info, warn or error",
            ),
            AppConfigCase(
                name = "AUTH_LOCAL_ENABLED: '0', AUTH_GOOGLE_ENABLED: 'false'",
                input = TestConfigDefaults.testConfigBase(
                    "AUTH_LOCAL_ENABLED" to "0",
                    "AUTH_GOOGLE_ENABLED" to "false",
                ),
                expectedMessage = "no identity provider enabled: set AUTH_LOCAL_ENABLED or AUTH_GOOGLE_ENABLED",
            ),
            AppConfigCase(
                name = "SESSION_TTL: '96h', SESSION_MAX_LIFETIME: '48h'",
                input = TestConfigDefaults.testConfigBase(
                    "SESSION_TTL" to "96h",
                    "SESSION_MAX_LIFETIME" to "48h",
                ),
                expectedMessage = "SESSION_MAX_LIFETIME ('PT48H') is shorter than SESSION_TTL ('PT96H')",
            ),
            AppConfigCase(
                name = "SESSION_TTL: '30d'",
                input = TestConfigDefaults.testConfigBase("SESSION_TTL" to "30d"),
                expectedMessage = "SESSION_TTL: '30d' is not a valid duration",
            ),
            AppConfigCase(
                name = "HTTP_READ_TIMEOUT: 'abc'",
                input = TestConfigDefaults.testConfigBase("HTTP_READ_TIMEOUT" to "abc"),
                expectedMessage = "HTTP_READ_TIMEOUT: 'abc' is not a valid duration",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: 'banana'",
                input = TestConfigDefaults.testConfigBase("AUTO_MIGRATE" to "banana"),
                expectedMessage = "AUTO_MIGRATE: 'banana' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: '2'",
                input = TestConfigDefaults.testConfigBase("AUTO_MIGRATE" to "2"),
                expectedMessage = "AUTO_MIGRATE: '2' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: ' true'",
                input = TestConfigDefaults.testConfigBase("AUTO_MIGRATE" to " true"),
                expectedMessage = "AUTO_MIGRATE: ' true' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: ''",
                input = TestConfigDefaults.testConfigBase("AUTO_MIGRATE" to ""),
                expectedMessage = "AUTO_MIGRATE: '' is not a boolean",
            ),
            AppConfigCase(
                name = "LOG_FORMAT: ' json'",
                input = TestConfigDefaults.testConfigBase("LOG_FORMAT" to " json"),
                expectedMessage = "LOG_FORMAT ' json': want text or json",
            ),
            AppConfigCase(
                name = "HTTP_WRITE_TIMEOUT: '0s'",
                input = TestConfigDefaults.testConfigBase("HTTP_WRITE_TIMEOUT" to "0s"),
                expectedMessage = "HTTP_WRITE_TIMEOUT 'PT0S': must be positive",
            ),
            AppConfigCase(
                name = "HTTP_WRITE_TIMEOUT: '-1s'",
                input = TestConfigDefaults.testConfigBase("HTTP_WRITE_TIMEOUT" to "-1s"),
                expectedMessage = "HTTP_WRITE_TIMEOUT 'PT-1S': must be positive",
            ),
            // No backoff at all is not a backoff, and a cap below the first
            // window would make the second failure wait less than the first.
            // Go refuses the same three (config_test.go).
            AppConfigCase(
                name = "LOGIN_FIRST_BACKOFF: '0s'",
                input = TestConfigDefaults.testConfigBase("LOGIN_FIRST_BACKOFF" to "0s"),
                expectedMessage = "LOGIN_FIRST_BACKOFF 'PT0S': must be at least 1ms",
            ),
            AppConfigCase(
                name = "LOGIN_FIRST_BACKOFF: '-1s'",
                input = TestConfigDefaults.testConfigBase("LOGIN_FIRST_BACKOFF" to "-1s"),
                expectedMessage = "LOGIN_FIRST_BACKOFF 'PT-1S': must be at least 1ms",
            ),
            // #69: the floor is 1ms, not "positive" - a value Go's time.Duration
            // can express but neither backend's login path resolves to.
            AppConfigCase(
                name = "LOGIN_FIRST_BACKOFF: '999us'",
                input = TestConfigDefaults.testConfigBase("LOGIN_FIRST_BACKOFF" to "999us"),
                expectedMessage = "LOGIN_FIRST_BACKOFF 'PT0.000999S': must be at least 1ms",
            ),
            // S3 (second review of #69): whitespace-only is not the same as
            // unset - requiredDuration's ifEmpty only special-cases the exact
            // empty string, matching Go's caarlos0/env, so this reaches
            // DurationParser and is refused like any other unparseable
            // spelling, not silently defaulted.
            AppConfigCase(
                name = "LOGIN_FIRST_BACKOFF: '   ' (whitespace is not unset)",
                input = TestConfigDefaults.testConfigBase("LOGIN_FIRST_BACKOFF" to "   "),
                expectedMessage = "LOGIN_FIRST_BACKOFF: '   ' is not a valid duration",
            ),
            AppConfigCase(
                name = "LOGIN_FIRST_BACKOFF: '\\t' (a tab is not unset)",
                input = TestConfigDefaults.testConfigBase("LOGIN_FIRST_BACKOFF" to "\t"),
                expectedMessage = "LOGIN_FIRST_BACKOFF: '\t' is not a valid duration",
            ),
            AppConfigCase(
                name = "LOGIN_MAX_BACKOFF: '500ms'",
                input = TestConfigDefaults.testConfigBase("LOGIN_MAX_BACKOFF" to "500ms"),
                expectedMessage = "LOGIN_MAX_BACKOFF ('PT0.5S') is shorter than LOGIN_FIRST_BACKOFF ('PT1S')",
            ),
        )
    ) { (_, input, _, expectedMessage) ->
        val ex = shouldThrow<ConfigException> { AppConfig.parse(input) }
        ex.message shouldBe expectedMessage
    }

    // The backoff defaults are the parameters contract/testdata/login_backoff.json
    // computes its curve from (#16). Go's config_test.go holds its defaults to
    // the same file, so a default changed in one backend fails there.
    "backoff defaults match the shared fixture" {
        val parameters = JsonMapper()
            .readTree(File(System.getProperty("afloat.contract.testdata"), "login_backoff.json"))
            .get("parameters")
        val cfg = AppConfig.parse(TestConfigDefaults.testConfigBase())

        cfg.loginFirstBackoff shouldBe DurationParser.parse(parameters.get("first_backoff").asString())
        cfg.loginMaxBackoff shouldBe DurationParser.parse(parameters.get("max_backoff").asString())
    }
})
