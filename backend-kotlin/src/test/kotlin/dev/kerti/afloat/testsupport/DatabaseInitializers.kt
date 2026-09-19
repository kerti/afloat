package dev.kerti.afloat.testsupport

import org.springframework.context.ApplicationContextInitializer
import org.springframework.context.ConfigurableApplicationContext
import org.springframework.core.env.MapPropertySource

// Each distinct DATABASE_URL needs its own initializer class. Spring keys the
// context cache on the initializer classes, not the values they inject, so two
// specs sharing a class would share one ApplicationContext (and one database).
private fun ConfigurableApplicationContext.useDatabase(
    url: String,
    vararg overrides: Pair<String, String>,
) {
    environment.propertySources.addFirst(
        MapPropertySource("afloatTestDatabase", mapOf("DATABASE_URL" to url) + overrides)
    )
}

// The migrated shared database: the default for specs that need a schema.
class SharedDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(TestDatabase.shared())
    }
}

// A freshly created, unmigrated database, so Flyway's baseline can be observed.
class VirginDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(TestDatabase.virgin())
    }
}

// A fresh database with migrations switched off, so a spec can assert Flyway
// did not run. AUTO_MIGRATE=false is enough on its own: FlywayEnablementContextInitializer
// feeds the parsed value into spring.flyway.enabled, which is exactly the path
// this exercises. spring.jpa.hibernate.ddl-auto=none is the time bomb: with the
// YAML default of validate, JPA would abort the context on the empty schema
// before the assertion ever executed.
class NoMigrateDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.noMigrate(),
            "AUTO_MIGRATE" to "false",
            "spring.jpa.hibernate.ddl-auto" to "none",
        )
    }
}

// The reported regression end-to-end: an AUTO_MIGRATE spelling that passes the
// Go-parity parse (1/t/T) must still turn Flyway ON against a virgin database.
class NumericEnableDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.virginNumeric(),
            "AUTO_MIGRATE" to "1",
        )
    }
}
