package dev.kerti.afloat

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient

class CleanDatabaseSpec : DatabaseSpec() {

    init {
        "a write is visible within the test that made it" {
            val client = JdbcClient.create(dataSource)
            client.sql("INSERT INTO login_attempts (key) VALUES ('ip:203.0.113.1')").update()
            client.sql("SELECT count(*) FROM login_attempts")
                .query(Long::class.java).single() shouldBe 1L
        }

        "eachTestSeesACleanDatabase" {
            val client = JdbcClient.create(dataSource)
            client.sql("SELECT count(*) FROM login_attempts")
                .query(Long::class.java).single() shouldBe 0L
        }
    }
}
