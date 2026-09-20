package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.TestConfigDefaults
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.shouldBe
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
                    cookieSecure = true,
                    version = "dev",
                )
            ),
            AppConfigCase(
                name = "PORT: '0' means OS-assigned (Boot's RANDOM_PORT web tests write it)",
                input = TestConfigDefaults.testConfigBase("PORT" to "0"),
                expectedConfig = AppConfig(
                    databaseUrl = "postgres://host/afloat_kotlin",
                    port = 0,
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
                    cookieSecure = true,
                    version = "dev",
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
                name = "HTTP_READ_TIMEOUT: '1m30s'",
                input = TestConfigDefaults.testConfigBase("HTTP_READ_TIMEOUT" to "1m30s"),
                expectedMessage = "HTTP_READ_TIMEOUT: '1m30s' must be a single-component duration (e.g. 30s or 1h)",
            ),
            AppConfigCase(
                name = "HTTP_READ_TIMEOUT: '1.5h'",
                input = TestConfigDefaults.testConfigBase("HTTP_READ_TIMEOUT" to "1.5h"),
                expectedMessage = "HTTP_READ_TIMEOUT: '1.5h' must be a single-component duration (e.g. 30s or 1h)",
            ),
            AppConfigCase(
                name = "SESSION_TTL: '36h30m'",
                input = TestConfigDefaults.testConfigBase("SESSION_TTL" to "36h30m"),
                expectedMessage = "SESSION_TTL: '36h30m' must be a single-component duration (e.g. 30s or 1h)",
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
        )
    ) { (_, input, _, expectedMessage) ->
        val ex = shouldThrow<ConfigException> { AppConfig.parse(input) }
        ex.message shouldBe expectedMessage
    }
})
