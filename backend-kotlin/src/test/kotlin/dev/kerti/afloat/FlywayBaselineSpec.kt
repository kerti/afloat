package dev.kerti.afloat

import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import dev.kerti.afloat.testsupport.VirginDatabaseInitializer
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [VirginDatabaseInitializer::class])
class FlywayBaselineSpec : SpringDatabaseSpec() {

    init {
        "flywayAppliesBaselineOnStart" {
            val client = JdbcClient.create(dataSource)
            val versions = client.sql(
                "SELECT version FROM flyway_schema_history WHERE success = true AND type = 'SQL'"
            ).query(String::class.java).list()

            versions shouldBe listOf("0001")
        }
    }
}
