package dev.kerti.afloat.system

import org.slf4j.LoggerFactory
import org.springframework.jdbc.core.simple.JdbcClient
import org.springframework.stereotype.Component

fun interface DatabaseProbe { fun isReachable(): Boolean }

@Component
class JdbcDatabaseProbe(private val jdbc: JdbcClient) : DatabaseProbe {
    private val log = LoggerFactory.getLogger(JdbcDatabaseProbe::class.java)

    override fun isReachable(): Boolean = try {
        jdbc.sql("SELECT 1").query(Int::class.java).single() == 1
    } catch (e: Exception) {
        log.error("health: database unreachable", e)
        false
    }
}
