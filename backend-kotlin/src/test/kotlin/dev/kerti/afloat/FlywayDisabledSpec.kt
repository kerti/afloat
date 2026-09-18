package dev.kerti.afloat

import dev.kerti.afloat.testsupport.NoMigrateDatabaseInitializer
import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [NoMigrateDatabaseInitializer::class])
class FlywayDisabledSpec : SpringDatabaseSpec() {

    init {
        "flywayDoesNotRunWhenAutoMigrateDisabled" {
            val client = JdbcClient.create(dataSource)
            val historyTables = client.sql(
                "SELECT count(*) FROM information_schema.tables " +
                        "WHERE table_schema = 'public' AND table_name = 'flyway_schema_history'"
            ).query(Long::class.java).single()

            historyTables shouldBe 0L
        }
    }
}
