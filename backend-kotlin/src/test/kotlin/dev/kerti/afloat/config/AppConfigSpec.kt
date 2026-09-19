package dev.kerti.afloat.config

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
                name = "Defaults",
                input = mapOf("DATABASE_URL" to "postgres://host/afloat_kotlin"),
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
                name = "Overrides and tolerant bools", input = mapOf(
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
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "VERSION" to " ",
                ),
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
        ),
    ) { (_, input, expectedConfig) ->
        val cfg = AppConfig.load(input)
        cfg shouldBe expectedConfig
    }

    withData(
        nameFn = { it.name }, listOf(
            AppConfigCase(
                name = "Empty map",
                input = emptyMap(),
                expectedMessage = "DATABASE_URL is empty",
            ),
            AppConfigCase(
                name = "DATABASE_URL: ''",
                input = mapOf("DATABASE_URL" to "",),
                expectedMessage = "DATABASE_URL is empty",
            ),
            AppConfigCase(
                name = "DATABASE_URL: '    '",
                input = mapOf("DATABASE_URL" to "    ",),
                expectedMessage = "DATABASE_URL is empty",
            ),
            AppConfigCase(
                name = "PORT: '0'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "PORT" to "0",
                ),
                expectedMessage = "PORT '0' is not a valid port",
            ),
            AppConfigCase(
                name = "PORT: '70000'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "PORT" to "70000",
                ),
                expectedMessage = "PORT '70000' is not a valid port",
            ),
            AppConfigCase(
                name = "PORT: 'abc'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "PORT" to "abc",
                ),
                expectedMessage = "PORT: 'abc' is not a valid integer",
            ),
            AppConfigCase(
                name = "LOG_FORMAT: 'yaml'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "LOG_FORMAT" to "yaml",
                ),
                expectedMessage = "LOG_FORMAT 'yaml': want text or json",
            ),
            AppConfigCase(
                name = "LOG_LEVEL: 'verbose'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "LOG_LEVEL" to "verbose",
                ),
                expectedMessage = "LOG_LEVEL 'verbose': want debug, info, warn or error",
            ),
            AppConfigCase(
                name = "AUTH_LOCAL_ENABLED: '0', AUTH_GOOGLE_ENABLED: 'false'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "AUTH_LOCAL_ENABLED" to "0",
                    "AUTH_GOOGLE_ENABLED" to "false",

                    ),
                expectedMessage = "no identity provider enabled: set AUTH_LOCAL_ENABLED or AUTH_GOOGLE_ENABLED",
            ),
            AppConfigCase(
                name = "SESSION_TTL: '96h', SESSION_MAX_LIFETIME: '48h'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "SESSION_TTL" to "96h",
                    "SESSION_MAX_LIFETIME" to "48h",

                    ),
                expectedMessage = "SESSION_MAX_LIFETIME ('PT48H') is shorter than SESSION_TTL ('PT96H')",
            ),
            AppConfigCase(
                name = "SESSION_TTL: '30d'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "SESSION_TTL" to "30d",
                ),
                expectedMessage = "SESSION_TTL: '30d' is not a valid duration",
            ),
            AppConfigCase(
                name = "HTTP_READ_TIMEOUT: 'abc'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "HTTP_READ_TIMEOUT" to "abc",
                ),
                expectedMessage = "HTTP_READ_TIMEOUT: 'abc' is not a valid duration",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: 'banana'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "AUTO_MIGRATE" to "banana",
                ),
                expectedMessage = "AUTO_MIGRATE: 'banana' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: '2'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "AUTO_MIGRATE" to "2",
                ),
                expectedMessage = "AUTO_MIGRATE: '2' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: ' true'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "AUTO_MIGRATE" to " true",
                ),
                expectedMessage = "AUTO_MIGRATE: ' true' is not a boolean",
            ),
            AppConfigCase(
                name = "AUTO_MIGRATE: ''",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "AUTO_MIGRATE" to "",
                ),
                expectedMessage = "AUTO_MIGRATE: '' is not a boolean",
            ),
            AppConfigCase(
                name = "LOG_FORMAT: ' json'",
                input = mapOf(
                    "DATABASE_URL" to "postgres://host/afloat_kotlin",
                    "LOG_FORMAT" to " json",
                ),
                expectedMessage = "LOG_FORMAT ' json': want text or json",
            ),
        )
    ) { (_, input, _, expectedMessage) ->
        val ex = shouldThrow<ConfigException> { AppConfig.load(input) }
        ex.message shouldBe expectedMessage
    }
})