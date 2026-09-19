package dev.kerti.afloat.testsupport

import dev.kerti.afloat.config.PostgresUrlTranslator
import org.springframework.jdbc.core.simple.JdbcClient
import org.testcontainers.postgresql.PostgreSQLContainer
import org.testcontainers.utility.DockerImageName
import java.sql.DriverManager
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import javax.sql.DataSource

enum class TestDbDecision { PROCEED, SKIP, FAIL }

object TestDatabase {

    // Must match docker-compose.yml:20.
    const val IMAGE = "postgres:18-alpine"

    private const val USER = "afloat"
    private const val PASSWORD = "afloat"
    private const val SHARED_DB = "afloat_test"
    private const val ADMIN_DB = "postgres"
    private const val REQUIRE_ENV = "AFLOAT_REQUIRE_TEST_DB"

    private val container = AtomicReference<PostgreSQLContainer?>(null)
    private val failure = AtomicReference<Throwable?>(null)
    private val starts = AtomicInteger(0)
    private var attempted = false

    // A second successful start would bump this; kept for
    // postgresContainerIsStartedOnceForTheWholeSuite.
    val startCount: Int get() = starts.get()

    @Synchronized
    fun start(): Boolean {
        container.get()?.let { return true }
        if (attempted) return failure.get() == null
        attempted = true

        return try {
            val started = PostgreSQLContainer(DockerImageName.parse(IMAGE))
                .withDatabaseName(SHARED_DB)
                .withUsername(USER)
                .withPassword(PASSWORD)
            // PostgreSQLContainer's built-in wait is the two "ready to accept
            // connections" log lines, i.e. Go's wait.ForLog(...).WithOccurrence(2).
            // Reuse stays false: it belongs in ~/.testcontainers.properties, never here.
            started.start()
            container.set(started)
            starts.incrementAndGet()

            // The URL we publish must carry credentials. A jdbc:-form URL would take
            // PostgresUrlTranslator's passthrough branch and silently drop them, so
            // fail loudly here if that ever changes.
            check(PostgresUrlTranslator.translate(libpqUrl(SHARED_DB)).username == USER) {
                "test container URL did not round-trip credentials through PostgresUrlTranslator"
            }
            true
        } catch (t: Throwable) {
            failure.set(t)
            false
        }
    }

    // Go's contract (testutil/db.go:53-56): absent Docker skips, unless CI says otherwise.
    // Pure, so TestcontainersGuardSpec can pin all five branches with no Docker.
    fun decision(available: Boolean, requireTestDb: String?): TestDbDecision = when {
        available -> TestDbDecision.PROCEED
        requireTestDb == "1" -> TestDbDecision.FAIL
        else -> TestDbDecision.SKIP
    }

    // Gate for @EnabledIf(DockerOrRequired::class): start the shared container once,
    // then turn the Docker/require contract into an enabled/disabled answer.
    fun isEnabled(): Boolean =
        decision(start(), System.getenv(REQUIRE_ENV)) != TestDbDecision.SKIP

    fun shared(): String {
        startedOrFail()
        return libpqUrl(SHARED_DB)
    }

    fun virgin(): String = freshDatabase("afloat_virgin")

    fun noMigrate(): String = freshDatabase("afloat_no_migrate")

    // A built-in AutoMigrate numeric spec (AUTO_MIGRATE=1) needs its own virgin
    // database: contexts are keyed on their initializer class, but the databases
    // must not collide, or a second boot lands on an already-migrated schema.
    fun virginNumeric(): String = freshDatabase("afloat_virgin_numeric")

    private fun freshDatabase(name: String): String {
        startedOrFail()
        createDatabaseIfAbsent(name)
        return libpqUrl(name)
    }

    private fun startedOrFail(): PostgreSQLContainer {
        when (decision(start(), System.getenv(REQUIRE_ENV))) {
            TestDbDecision.PROCEED -> Unit
            TestDbDecision.FAIL -> throw IllegalStateException(
                "testutil: no test database, and $REQUIRE_ENV=1", failure.get()
            )
            // Unreachable through @EnabledIf; means a spec lost the annotation.
            TestDbDecision.SKIP -> throw IllegalStateException(
                "testutil: no test database; this spec should have been skipped by @EnabledIf"
            )
        }
        return checkNotNull(container.get())
    }

    private fun libpqUrl(database: String): String {
        val c = checkNotNull(container.get()) { "TestDatabase not started" }
        return "postgres://$USER:$PASSWORD@${c.host}:${c.firstMappedPort}/$database?sslmode=disable"
    }

    // A brand-new database has no flyway_schema_history, so it is as virgin as a
    // second container, for one DDL statement. The afloat role is the image superuser.
    @Synchronized
    private fun createDatabaseIfAbsent(name: String) {
        val c = checkNotNull(container.get())
        DriverManager.getConnection(
            "jdbc:postgresql://${c.host}:${c.firstMappedPort}/$ADMIN_DB", USER, PASSWORD
        ).use { conn ->
            val exists = conn.prepareStatement("SELECT 1 FROM pg_database WHERE datname = ?").use { ps ->
                ps.setString(1, name)
                ps.executeQuery().use { it.next() }
            }
            if (!exists) conn.createStatement().use { it.execute("""CREATE DATABASE "$name" """) }
        }
    }

    // From the catalog, so a new migration's tables are swept with no change here.
    // flyway_schema_history is left alone, or the next boot would re-migrate.
    fun truncate(dataSource: DataSource) {
        val client = JdbcClient.create(dataSource)
        val tables = client.sql(
            "SELECT tablename FROM pg_tables " +
                    "WHERE schemaname = 'public' AND tablename <> 'flyway_schema_history'"
        ).query(String::class.java).list()
        check(tables.isNotEmpty()) { "testutil: no application tables found; did migrations run?" }
        client.sql("TRUNCATE " + tables.joinToString(", ") + " RESTART IDENTITY CASCADE").update()
    }
}
