package dev.kerti.afloat

import dev.kerti.afloat.testsupport.NumericEnableDatabaseInitializer
import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.ContextConfiguration

// The reported regression, end-to-end: AUTO_MIGRATE=1 passes AppConfig's
// Go-parity parse, and must turn Flyway ON, not off. The Flyway
// @ConditionalOnBooleanProperty only prizes the literal "true", so the lazy
// normalized property is what gets this precedent - an explicit spring.flyway
// .enabled override is deliberately absent here.
@ContextConfiguration(initializers = [NumericEnableDatabaseInitializer::class])
class FlywayEnabledByNumericValueSpec : SpringDatabaseSpec() {

    init {
        "flywayMigratesWhenAutoMigrateSpelledAsOne" {
            val client = JdbcClient.create(dataSource)
            val versions = client.sql(
                "SELECT version FROM flyway_schema_history WHERE success = true AND type = 'SQL'"
            ).query(String::class.java).list()

            versions shouldBe listOf("0001")
        }
    }
}