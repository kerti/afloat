package dev.kerti.afloat.config

import dev.kerti.afloat.testsupport.SpringDatabaseSpec
import dev.kerti.afloat.testsupport.StatementTimeoutDatabaseInitializer
import io.kotest.assertions.throwables.shouldNotThrowAny
import io.kotest.assertions.throwables.shouldThrow
import io.kotest.assertions.withClue
import io.kotest.matchers.longs.shouldBeLessThan
import io.kotest.matchers.shouldBe
import org.springframework.beans.factory.annotation.Autowired
import org.springframework.test.context.ContextConfiguration
import java.sql.SQLException

// Postgres's query_canceled: what the server answers when the JDBC driver
// cancels a statement whose queryTimeout ran out.
private const val QUERY_CANCELED = "57014"

private fun Throwable.wasCancelledByTimeout(): Boolean =
    generateSequence(this) { it.cause }
        .filterIsInstance<SQLException>()
        .any { it.sqlState == QUERY_CANCELED }

@ContextConfiguration(initializers = [StatementTimeoutDatabaseInitializer::class])
class DatabaseTimeoutSpec : SpringDatabaseSpec() {

    @Autowired
    private lateinit var slow: SlowStatementProbe

    @Autowired
    private lateinit var slowRepository: SlowStatementRepository

    init {
        "allows a statement that does not overrun HTTP_WRITE_TIMEOUT" {
            val startedAt = System.nanoTime()
            shouldNotThrowAny { slow.sleep(seconds = 2) }
            val elapsedSeconds = (System.nanoTime() - startedAt) / 1_000_000_000

            elapsedSeconds shouldBeLessThan 3
        }

        "cuts a statement that overruns HTTP_WRITE_TIMEOUT" {
            val startedAt = System.nanoTime()
            val e = shouldThrow<Exception> { slow.sleep(seconds = 30) }
            val elapsedSeconds = (System.nanoTime() - startedAt) / 1_000_000_000
            withClue(e.toString()) { e.wasCancelledByTimeout() shouldBe true }

            elapsedSeconds shouldBeLessThan 5
        }

        "cuts a declared query on an annotated interface" {
            val startedAt = System.nanoTime()
            val e = shouldThrow<Exception> { slowRepository.sleep(seconds = 30) }
            val elapsedSeconds = (System.nanoTime() - startedAt) / 1_000_000_000
            withClue(e.toString()) { e.wasCancelledByTimeout() shouldBe true }

            elapsedSeconds shouldBeLessThan 5
        }
    }
}
