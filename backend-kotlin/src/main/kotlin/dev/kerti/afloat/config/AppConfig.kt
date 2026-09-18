package dev.kerti.afloat.config

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
        // The names the adapter reads from the Environment. It is kept here, next to
        // load(), so the set and the reader cannot drift. AppConfigEnvNamesSpec pins
        // it against application.yaml's placeholders.
        val ENV_NAMES = listOf(
            "DATABASE_URL",
            "PORT",
            "LOG_FORMAT",
            "LOG_LEVEL",
            "AUTO_MIGRATE",
            "HTTP_READ_TIMEOUT",
            "HTTP_WRITE_TIMEOUT",
            "HTTP_IDLE_TIMEOUT",
            "SHUTDOWN_TIMEOUT",
            "AUTH_LOCAL_ENABLED",
            "AUTH_GOOGLE_ENABLED",
            "SESSION_TTL",
            "SESSION_MAX_LIFETIME",
            "COOKIE_SECURE",
            "VERSION",
        )

        // We do not parse the URL here, only guard against absence or set-but-empty.
        // The data connection layer will validate it later in the app lifecycle.
        fun load(env: Map<String, String>): AppConfig {
            val cfg = AppConfig(
                databaseUrl = env["DATABASE_URL"] ?: "",
                port = env["PORT"]?.let {
                    it.toIntOrNull() ?: throw ConfigException("PORT: '$it' is not a valid integer")
                } ?: 5183,
                logFormat = env["LOG_FORMAT"] ?: "text",
                logLevel = env["LOG_LEVEL"] ?: "info",
                autoMigrate = (env["AUTO_MIGRATE"])?.let { parseBool("AUTO_MIGRATE", it) } ?: true,
                readTimeout = env["HTTP_READ_TIMEOUT"]?.let { parseDuration("HTTP_READ_TIMEOUT", it) }
                    ?: Duration.ofSeconds(30),
                writeTimeout = env["HTTP_WRITE_TIMEOUT"]?.let { parseDuration("HTTP_WRITE_TIMEOUT", it) }
                    ?: Duration.ofSeconds(60),
                idleTimeout = env["HTTP_IDLE_TIMEOUT"]?.let { parseDuration("HTTP_IDLE_TIMEOUT", it) }
                    ?: Duration.ofSeconds(120),
                shutdownTimeout = env["SHUTDOWN_TIMEOUT"]?.let { parseDuration("SHUTDOWN_TIMEOUT", it) }
                    ?: Duration.ofSeconds(10),
                authLocalEnabled = (env["AUTH_LOCAL_ENABLED"])?.let { parseBool("AUTH_LOCAL_ENABLED", it) } ?: true,
                authGoogleEnabled = (env["AUTH_GOOGLE_ENABLED"])?.let { parseBool("AUTH_GOOGLE_ENABLED", it) } ?: false,
                sessionTtl = env["SESSION_TTL"]?.let { parseDuration("SESSION_TTL", it) } ?: Duration.ofHours(720),
                sessionMaxLifetime = env["SESSION_MAX_LIFETIME"]?.let { parseDuration("SESSION_MAX_LIFETIME", it) }
                    ?: Duration.ofHours(2160),
                cookieSecure = (env["COOKIE_SECURE"])?.let { parseBool("COOKIE_SECURE", it) } ?: true,
                version = env["VERSION"]?.takeIf { it.isNotBlank() } ?: "dev",
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

            if (cfg.port !in 1..65535) {
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

        private fun parseBool(name: String, raw: String): Boolean = when (raw.lowercase()) {
            "0", "f", "false" -> false
            "1", "t", "true" -> true
            else -> throw ConfigException("$name: '$raw' is not a boolean")
        }

        private fun parseDuration(name: String, raw: String): Duration = try {
            DurationParser.parse(raw)
        } catch (e: IllegalArgumentException) {
            throw ConfigException("$name: '${raw}' is not a valid duration")
        }

    }
}