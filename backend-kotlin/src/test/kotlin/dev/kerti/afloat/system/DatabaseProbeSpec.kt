package dev.kerti.afloat.system

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.matchers.shouldBe
import org.springframework.jdbc.core.simple.JdbcClient
import java.io.PrintWriter
import java.lang.reflect.Proxy
import java.sql.Connection
import java.sql.SQLException
import java.util.logging.Logger
import javax.sql.DataSource

// healthRunsARealQueryNotAPing (#13 test 10)
//
// JdbcDatabaseProbe is what actually decides whether GET /api/health answers ok
// or degraded, and every other spec replaces it with a lambda — so nothing
// exercised the class itself. Two properties matter and neither is visible from
// SystemControllerSpec:
//
//   - It EXECUTES and SCANS a statement rather than taking a connection and
//     handing it back. A pool pointed at a database this role cannot read gives
//     out a connection happily and fails on the statement; that difference is
//     liveness versus readiness, and it is why Go's Ping is `SELECT 1` through
//     the query path rather than pgx's own Ping (backend/queries/health.sql).
//   - It answers FALSE rather than throwing. Health reports a downed database as
//     a 503 body, not an error envelope, so an exception escaping the probe
//     would turn a degraded instance into a 500 INTERNAL.
class DatabaseProbeSpec : DatabaseSpec() {

    // Fails at getConnection, the way an unreachable host or an exhausted pool
    // does, but instantly.
    private object UnreachableDataSource : DataSource {
        override fun getConnection(): Connection = throw SQLException("connection refused")
        override fun getConnection(username: String?, password: String?): Connection = connection
        override fun getLogWriter(): PrintWriter? = null
        override fun setLogWriter(out: PrintWriter?) = Unit
        override fun setLoginTimeout(seconds: Int) = Unit
        override fun getLoginTimeout(): Int = 0
        override fun getParentLogger(): Logger = Logger.getGlobal()
        override fun <T : Any?> unwrap(iface: Class<T>?): T = throw SQLException("not a wrapper")
        override fun isWrapperFor(iface: Class<*>?): Boolean = false
    }

    // Hands out a real, working connection and fails only when something tries
    // to run SQL on it. This is the shape a ping cannot tell from healthy.
    private fun statementRefusingDataSource(delegate: DataSource): DataSource =
        object : DataSource by delegate {
            override fun getConnection(): Connection {
                val real = delegate.connection
                return Proxy.newProxyInstance(
                    javaClass.classLoader,
                    arrayOf(Connection::class.java),
                ) { _, method, args ->
                    when (method.name) {
                        "prepareStatement", "createStatement", "prepareCall" ->
                            throw SQLException("permission denied for relation")
                        else -> method.invoke(real, *(args ?: emptyArray()))
                    }
                } as Connection
            }
        }

    init {
        "reports reachable when the query runs and returns its row" {
            JdbcDatabaseProbe(JdbcClient.create(dataSource)).isReachable() shouldBe true
        }

        "reports unreachable, without throwing, when no connection can be had" {
            JdbcDatabaseProbe(JdbcClient.create(UnreachableDataSource)).isReachable() shouldBe false
        }

        // The liveness-versus-readiness case, and the whole reason this is a
        // query and not a ping: the connection is fine and the statement is not.
        "reports unreachable when a connection opens but the statement fails" {
            val probe = JdbcDatabaseProbe(JdbcClient.create(statementRefusingDataSource(dataSource)))

            probe.isReachable() shouldBe false
        }
    }
}
