package dev.kerti.afloat.config

import org.springframework.core.env.Environment
import org.springframework.core.env.PropertySource

// Boot binds server.tomcat.connection-timeout, server.tomcat.keep-alive-timeout
// and spring.lifecycle.timeout-per-shutdown-phase from whatever string reaches
// it. With the raw ${HTTP_READ_TIMEOUT:...} relay that spellings Go's
// time.ParseDuration accepts (1m30s, 1.5h) come out different here: Spring's
// simple duration style cannot take them, so HTTP_READ_TIMEOUT=1m30s boots Go
// and kills Kotlin. Normalise rather than restrict — parse with the Go-parity
// DurationParser and hand Boot the parsed ISO-8601 (PT1M30S), which its ISO8601
// style binds exactly.
//
// Values are read from the resolved afloat leaves — the same source AppConfig
// binds — never a raw env name, so a default or an override is seen identically
// by the bean and by this relay.
//
// Property-source hygiene mirrors AutoMigrateFlywayPropertySource: the
// Environment is a plain constructor property, never `source`, and this source
// answers nothing but the three property keys above, never afloat.* itself.
class NormalizedServerTimeoutPropertySource(private val environment: Environment) :
    PropertySource<String>("afloatNormalizedServerTimeouts", "durations") {

    data class Relay(val leaf: String, val envName: String)

    private val relays: Map<String, Relay> = mapOf(
        "server.tomcat.connection-timeout" to Relay("afloat.http-read-timeout", "HTTP_READ_TIMEOUT"),
        "server.tomcat.keep-alive-timeout" to Relay("afloat.http-idle-timeout", "HTTP_IDLE_TIMEOUT"),
        "spring.lifecycle.timeout-per-shutdown-phase" to Relay("afloat.shutdown-timeout", "SHUTDOWN_TIMEOUT"),
    )

    override fun getProperty(name: String): Any? {
        val relay = relays[name] ?: return null
        val raw = environment.getProperty(relay.leaf) ?: return null
        return try {
            DurationParser.parse(raw).toString()
        } catch (e: IllegalArgumentException) {
            throw ConfigException("${relay.envName}: '${raw}' is not a valid duration")
        }
    }
}
