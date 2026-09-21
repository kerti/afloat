package dev.kerti.afloat.testsupport

import org.springframework.boot.env.YamlPropertySourceLoader
import org.springframework.core.env.Environment
import org.springframework.core.env.MapPropertySource
import org.springframework.core.env.StandardEnvironment
import org.springframework.core.io.ClassPathResource

// application.yaml owns the defaults (BOOTSTRAP.md §12 pins it against the
// cross-backend table); these helpers derive test inputs from it so a spec
// cannot quietly re-pin a stale literal the very gate already watches.
object TestConfigDefaults {

    private val placeholder = Regex("""\$\{([A-Z0-9_]+):([^}]*)}""")

    // name -> default string, exactly as the placeholders in application.yaml
    // declare them. DATABASE_URL's default is empty by design: it is required.
    // A name may legitimately appear in several leaves (LOG_LEVEL under logging:
    // and afloat:), but every occurrence must agree, or one reader has quietly
    // been given a different default from another. check-env-parity.sh now
    // compares every occurrence too; this stays because it fails at the point
    // the stale value would be read, with the name in the message.
    fun scrapedDefaults(): Map<String, String> {
        val yaml = checkNotNull(javaClass.getResource("/application.yaml")).readText()
        return placeholder.findAll(yaml).groupBy { it.groupValues[1] }
            .mapValues { (name, matches) ->
                val distinct = matches.map { it.groupValues[2] }.distinct()
                check(distinct.size == 1) {
                    "application.yaml repeats \${$name} with conflicting defaults: ${distinct}"
                }
                distinct.first()
            }
    }

    // A complete, valid value map: the yaml defaults, with DATABASE_URL (which
    // has no default) set to something the validator accepts, plus overrides.
    fun testConfigBase(vararg overrides: Pair<String, String>): Map<String, String> =
        scrapedDefaults() - "DATABASE_URL" +
            (mapOf("DATABASE_URL" to "postgres://host/afloat_kotlin") + overrides)

    // A Spring Environment carrying application.yaml plus the given overrides,
    // with the machine's own sources excluded so a host variable or CI value
    // cannot leak into assertions about defaults.
    fun environment(vararg overrides: Pair<String, String>): Environment {
        val env = StandardEnvironment()
        with(env.propertySources) {
            toList().forEach { remove(it.name) }
            YamlPropertySourceLoader()
                .load("application.yaml", ClassPathResource("application.yaml"))
                .forEach { addLast(it) }
            addFirst(MapPropertySource("afloatTestConfig", mapOf(*overrides)))
        }
        return env
    }
}
