package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.TestConfigDefaults
import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import org.yaml.snakeyaml.Yaml

@Suppress("UNCHECKED_CAST")
open class RuntimeKnobsSpec : StringSpec({

    val root: Map<String, Any> =
        Yaml().load<Map<String, Any>>(javaClass.getResourceAsStream("/application.yaml"))
    val defaults = TestConfigDefaults.scrapedDefaults()

    // The gap AppConfig.bind-then-forget opened: every knob a runtime keeps in
    // AppConfig must reach the layer that uses it. Each leaf must resolve to the
    // exact placeholder and its default, so a resolved value or a dropped default
    // is called out. HTTP_WRITE_TIMEOUT is wired as the transaction manager's
    // default timeout (AppConfiguration.transactionManager, #30) - a bean, not a
    // Spring leaf - so it has no row here; LOG_FORMAT stays bound-but-deferred
    // (issue #13 §6: no logging sink yet) and is the one deliberate omission.
    //
    // Three of these knobs are marked relayed: NormalizedServerTimeoutPropertySource
    // is added first and answers their keys from the parsed afloat.* leaf, so at
    // runtime the placeholder below is a FALLBACK, not the live value. It is kept
    // deliberately - if that initializer ever stops being registered, boot degrades
    // to the raw relay carrying the §12 default, rather than to Boot's own
    // defaults (connection-timeout 20s), which nothing would catch.
    "the runtime knobs are wired from their env vars with their defaults" {
        data class Knob(val path: List<String>, val env: String, val default: String, val relayed: Boolean = false)

        val knobs = listOf(
            Knob(listOf("server", "port"), "PORT", defaults.getValue("PORT")),
            Knob(listOf("server", "tomcat", "connection-timeout"), "HTTP_READ_TIMEOUT", defaults.getValue("HTTP_READ_TIMEOUT"), relayed = true),
            Knob(listOf("server", "tomcat", "keep-alive-timeout"), "HTTP_IDLE_TIMEOUT", defaults.getValue("HTTP_IDLE_TIMEOUT"), relayed = true),
            Knob(listOf("spring", "lifecycle", "timeout-per-shutdown-phase"), "SHUTDOWN_TIMEOUT", defaults.getValue("SHUTDOWN_TIMEOUT"), relayed = true),
            Knob(listOf("logging", "level", "dev.kerti.afloat"), "LOG_LEVEL", defaults.getValue("LOG_LEVEL")),
        )

        knobs.forEach { knob ->
            resolve(root, knob.path) shouldBe "\${${knob.env}:${knob.default}}"
        }

        // The relayed rows above must be exactly the keys the normalizer serves:
        // a fourth relay, or one dropped, fails here instead of leaving a row
        // that documents a path nothing reads.
        knobs.filter { it.relayed }.map { it.path.joinToString(".") }
            .toSet() shouldBe NormalizedServerTimeoutPropertySource.RELAYED_KEYS
    }

    "shutdown is graceful so the phase timeout can bound the drain" {
        resolve(root, listOf("server", "shutdown")) shouldBe "graceful"
    }
}) {
    companion object {
        fun resolve(map: Map<String, Any>, path: List<String>): String? = when {
            path.isEmpty() -> null
            else -> {
                val child = map[path.first()]
                if (path.size == 1) child as? String
                else (child as? Map<String, Any>)?.let { resolve(it, path.drop(1)) }
            }
        }
    }
}
