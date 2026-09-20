package dev.kerti.afloat.config

import org.springframework.context.ApplicationContextInitializer
import org.springframework.context.ConfigurableApplicationContext
import kotlin.system.exitProcess

// An operator-legible goodbye before the bean layer builds. Registered only from
// main() - never via spring.factories, never discovered - so @SpringBootTest
// contexts cannot trip its exitProcess.
//
// An initializer runs after the Environment is fully resolved: process env,
// command-line args and spring.config.import all visible. The previous preflight
// in main() read System.getenv() alone, so a config Spring would have accepted
// (or rejected) exited 1 for the wrong reason, and either way the Flyway
// property source could still fail later under a source the preflight had
// never seen. One from() here settles both: everything an operator can set is
// checked, and the message is the ConfigException's own.
class ConfigPreflightInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        try {
            AppConfig.from(context.environment)
        } catch (e: ConfigException) {
            System.err.println(e.message)
            exitProcess(1)
        }
    }
}
