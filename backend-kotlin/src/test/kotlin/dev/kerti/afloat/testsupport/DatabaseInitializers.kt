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

// Each tolerant spelling gets its own initializer (and database): Spring keys
// the context cache on the initializer class, so sharing one would let a second
// row inherit the first row's boot and both rows would pass for the wrong reason.

// The reported regression end-to-end: a spelling that passes the Go-parity
// parse (1/t/T) must still turn Flyway ON against a virgin database.
class NumericEnableDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.virginNumeric(),
            "AUTO_MIGRATE" to "1",
        )
    }
}

class TEnableDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.virginT(),
            "AUTO_MIGRATE" to "t",
        )
    }
}

// 0 and f must keep Flyway off; ddl-auto=none is the same JPA time bomb guard
// as NoMigrateDatabaseInitializer.
class ZeroDisableDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.noMigrateZero(),
            "AUTO_MIGRATE" to "0",
            "spring.jpa.hibernate.ddl-auto" to "none",
        )
    }
}

class FDisableDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.noMigrateF(),
            "AUTO_MIGRATE" to "f",
            "spring.jpa.hibernate.ddl-auto" to "none",
        )
    }
}

// Compound and fractional spellings that Go's time.ParseDuration accepts and
// Spring's simple duration style does not. Its own initializer class, so this
// context is never handed to another spec (and never inherits one): the whole
// point is a boot that binds these three values. The shared, migrated database
// is enough - nothing here reads the schema.
class CompoundDurationDatabaseInitializer : ApplicationContextInitializer<ConfigurableApplicationContext> {
    override fun initialize(context: ConfigurableApplicationContext) {
        context.useDatabase(
            TestDatabase.shared(),
            "HTTP_READ_TIMEOUT" to "1m30s",
            "HTTP_IDLE_TIMEOUT" to "90m0s",
            "SHUTDOWN_TIMEOUT" to "1.5m",
        )
    }
}
