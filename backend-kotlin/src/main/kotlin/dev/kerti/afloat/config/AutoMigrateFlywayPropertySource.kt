package dev.kerti.afloat.config

import org.springframework.core.env.Environment
import org.springframework.core.env.PropertySource

// Resolves spring.flyway.enabled from the parsed AUTO_MIGRATE instead of the raw
// string. Flyway's auto-configuration gates on @ConditionalOnBooleanProperty,
// which accepts only a value equal to "true" (OnPropertyCondition.isMatch), so
// the Go-parity spellings 1/t/T pass AppConfig.load and then silently switch
// Flyway off if the placeholder relays them unparsed.
//
// Deliberately lazy: a property source is probed in order at condition-evaluation
// time, so the AUTO_MIGRATE this reads is whatever the environment holds *then* -
// including overrides an ApplicationContextInitializer added after this one.
//
// The Environment is held as a plain constructor property, never as `source`:
// SpringConfigurationPropertySources$SourcesIterator.fetchNext() recurses into
// any candidate whose getSource() is a ConfigurableEnvironment, so storing it
// there makes Boot's iterator reconsider this source forever and OOM.
class AutoMigrateFlywayPropertySource(private val environment: Environment) :
    PropertySource<String>("afloatNormalizedAutoMigrate", "AUTO_MIGRATE") {

    override fun getProperty(name: String): Any? {
        if (name != "spring.flyway.enabled") return null
        val raw = environment.getProperty("AUTO_MIGRATE") ?: return null
        return AppConfig.parseBool("AUTO_MIGRATE", raw).toString().lowercase()
    }
}