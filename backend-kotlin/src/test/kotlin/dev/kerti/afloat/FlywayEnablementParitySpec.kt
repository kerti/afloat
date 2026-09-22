package dev.kerti.afloat

import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient

// One row per tolerant AUTO_MIGRATE spelling, end to end. Each concrete
// subclass pairs one initializer - and its dedicated database - with the
// outcome the §12 contract promises: 1/t run Flyway against a virgin schema,
// 0/f/false keep it off. The wire-level quirk under test is exactly how Boot
// hears the spelling: Flyway gates on @ConditionalOnBooleanProperty, which
// prizes only the literal "true", so the lazy normalized value must present
// itself as a spelled-out boolean.
abstract class FlywayEnablementParitySpec(
    private val runsMigration: Boolean,
    private val rowName: String,
) : SpringDatabaseSpec() {

    init {
        rowName {
            val client = JdbcClient.create(dataSource)
            if (runsMigration) {
                val versions = client.sql(
                    "SELECT version FROM flyway_schema_history WHERE success = true AND type = 'SQL'"
                ).query(String::class.java).list()

                versions shouldBe listOf("0001")
            } else {
                val historyTables = client.sql(
                    "SELECT count(*) FROM information_schema.tables " +
                            "WHERE table_schema = 'public' AND table_name = 'flyway_schema_history'"
                ).query(Long::class.java).single()

                historyTables shouldBe 0L
            }
        }
    }
}