package dev.kerti.afloat.config

import org.springframework.core.env.Environment
import java.time.Duration

class ConfigException(message: String) : IllegalStateException(message)

data class AppConfig(
    val databaseUrl: String,
    val port: Int,
    val logFormat: String,
    val logLevel: String,
    val autoMigrate: Boolean,
    val readTimeout: Duration,
    val writeTimeout: Duration,
    val idleTimeout: Duration,
    val shutdownTimeout: Duration,
    val authLocalEnabled: Boolean,
    val authGoogleEnabled: Boolean,
    val sessionTtl: Duration,
    val sessionMaxLifetime: Duration,
    val cookieSecure: Boolean,
    val version: String,
) {
    companion object {
        // operator-facing name -> the Spring property key from() resolves it from.
        // Every key is a leaf of application.yaml (PORT is server.port, where Boot
        // itself binds it), and the yaml subtree owns the defaults: AppConfig holds
        // no literals, so there is exactly one set and it cannot drift from the
        // §12 table check-env-parity.sh gates. Kept next to from()/parse() so the
        // set and the readers cannot drift either; AppConfigEnvNamesSpec pins it
        // against application.yaml's placeholders.
        private val PROPERTY_KEYS: List<Pair<String, String>> = listOf(
            "DATABASE_URL" to "afloat.database-url",
            "PORT" to "server.port",
            "LOG_FORMAT" to "afloat.log-format",
            "LOG_LEVEL" to "afloat.log-level",
            "AUTO_MIGRATE" to "afloat.auto-migrate",
            "HTTP_READ_TIMEOUT" to "afloat.http-read-timeout",
            "HTTP_WRITE_TIMEOUT" to "afloat.http-write-timeout",
            "HTTP_IDLE_TIMEOUT" to "afloat.http-idle-timeout",
            "SHUTDOWN_TIMEOUT" to "afloat.shutdown-timeout",
            "AUTH_LOCAL_ENABLED" to "afloat.auth-local-enabled",
            "AUTH_GOOGLE_ENABLED" to "afloat.auth-google-enabled",
            "SESSION_TTL" to "afloat.session-ttl",
            "SESSION_MAX_LIFETIME" to "afloat.session-max-lifetime",
            "COOKIE_SECURE" to "afloat.cookie-secure",
            "VERSION" to "afloat.version",
        )

        // The operator-facing contract names (BOOTSTRAP.md §12), in table order.
        val ENV_NAMES: List<String> = PROPERTY_KEYS.map { it.first }

        // The bean and the preflight initializer both land here: the values a
        // Spring Environment resolves, which the yaml subtree's ${NAME:default}
        // placeholders fill in where no env var overrides them.
        fun from(env: Environment): AppConfig =
            parse(PROPERTY_KEYS.mapNotNull { (name, key) ->
                env.getProperty(key)?.let { name to it }
            }.toMap())

        // We do not parse the URL here, only guard against absence or set-but-empty.
        // The data connection layer will validate it later in the app lifecycle.
        // Defaults are not a concern of this function: they live in application.yaml
        // and reach parse() through from().
        fun parse(values: Map<String, String>): AppConfig {
            fun required(name: String): String = values[name]
                ?: throw ConfigException("$name is not set in application.yaml")

            val cfg = AppConfig(
                databaseUrl = values["DATABASE_URL"] ?: "",
                port = required("PORT").let {
                    it.toIntOrNull() ?: throw ConfigException("PORT: '$it' is not a valid integer")
                },
                logFormat = required("LOG_FORMAT"),
                logLevel = required("LOG_LEVEL"),
                autoMigrate = parseBool("AUTO_MIGRATE", required("AUTO_MIGRATE")),
                readTimeout = parseDuration("HTTP_READ_TIMEOUT", required("HTTP_READ_TIMEOUT")),
                writeTimeout = parseDuration("HTTP_WRITE_TIMEOUT", required("HTTP_WRITE_TIMEOUT")),
                idleTimeout = parseDuration("HTTP_IDLE_TIMEOUT", required("HTTP_IDLE_TIMEOUT")),
                shutdownTimeout = parseDuration("SHUTDOWN_TIMEOUT", required("SHUTDOWN_TIMEOUT")),
                authLocalEnabled = parseBool("AUTH_LOCAL_ENABLED", required("AUTH_LOCAL_ENABLED")),
                authGoogleEnabled = parseBool("AUTH_GOOGLE_ENABLED", required("AUTH_GOOGLE_ENABLED")),
                sessionTtl = parseDuration("SESSION_TTL", required("SESSION_TTL")),
                sessionMaxLifetime = parseDuration("SESSION_MAX_LIFETIME", required("SESSION_MAX_LIFETIME")),
                cookieSecure = parseBool("COOKIE_SECURE", required("COOKIE_SECURE")),
                version = required("VERSION").takeIf { it.isNotBlank() } ?: DEFAULT_VERSION,
            )
            validate(cfg)
            return cfg
        }

        private fun validate(cfg: AppConfig) {
            if (cfg.databaseUrl.trim().isBlank()) {
                throw ConfigException("DATABASE_URL is empty")
            }

            if (!cfg.authLocalEnabled && !cfg.authGoogleEnabled) {
                throw ConfigException("no identity provider enabled: set AUTH_LOCAL_ENABLED or AUTH_GOOGLE_ENABLED")
            }

            // Port 0 is Boot's "let the OS pick" marker - RANDOM_PORT web tests
            // write server.port=0 and it reaches parse() through PROPERTY_KEYS.
            // It must not be blamed on the app; any port an operator actually
            // chooses must still be a usable 1..65535.
            if (cfg.port !in 0..65535) {
                throw ConfigException("PORT '${cfg.port}' is not a valid port")
            }

            if (cfg.logFormat != "text" && cfg.logFormat != "json") {
                throw ConfigException("LOG_FORMAT '${cfg.logFormat}': want text or json")
            }

            if (cfg.sessionMaxLifetime < cfg.sessionTtl) {
                throw ConfigException("SESSION_MAX_LIFETIME ('${cfg.sessionMaxLifetime}') is shorter than SESSION_TTL ('${cfg.sessionTtl}')")
            }

            if (cfg.logLevel != "debug" && cfg.logLevel != "info" && cfg.logLevel != "warn" && cfg.logLevel != "error") {
                throw ConfigException("LOG_LEVEL '${cfg.logLevel}': want debug, info, warn or error")
            }
        }

        // Shared with AutoMigrateFlywayPropertySource: the §12 contract parses
        // AUTO_MIGRATE once, and Flyway must see the same answer.
        internal fun parseBool(name: String, raw: String): Boolean = when (raw.lowercase()) {
            "0", "f", "false" -> false
            "1", "t", "true" -> true
            else -> throw ConfigException("$name: '$raw' is not a boolean")
        }

        // The one default AppConfig cannot source from application.yaml: Spring
        // renders ${VERSION:dev} as the empty value when an operator sets VERSION=""
        // or " ", and Go's env lib likewise backs off to its default for a blank
        // var. Mirror the afloat.version leaf and the §12 VERSION row; the parity
        // gate pins the two documents against each other.
        private const val DEFAULT_VERSION = "dev"

        // DurationParser owns the whole §12 duration surface: whatever Go's
        // time.ParseDuration accepts (compound 1m30s, fractional 1.5h) parses
        // here. Of the six durations, the three that relay raw into Spring's
        // own binder are normalized to ISO-8601 by
        // NormalizedServerTimeoutPropertySource, so neither this validation nor
        // the relay ever rejects a spelling Go accepted.
        private fun parseDuration(name: String, raw: String): Duration {
            val duration = try {
                DurationParser.parse(raw)
            } catch (e: IllegalArgumentException) {
                throw ConfigException("$name: '${raw}' is not a valid duration")
            }
            return duration
        }

    }
}
