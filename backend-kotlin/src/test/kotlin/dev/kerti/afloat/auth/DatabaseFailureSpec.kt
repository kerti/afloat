package dev.kerti.afloat.auth

import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe
import org.springframework.dao.DataAccessResourceFailureException
import org.springframework.transaction.CannotCreateTransactionException
import org.springframework.transaction.IllegalTransactionStateException
import org.springframework.transaction.TransactionSystemException

// Which exceptions the auth catches treat as an outage (#23, #27), and which
// they rethrow. DatabaseOutageSpec proves the outage ones end where the ruling
// says; this pins the line between the two.
class DatabaseFailureSpec : StringSpec({

    "counts both families an outage arrives as" {
        DataAccessResourceFailureException("down").isDatabaseFailure() shouldBe true
        CannotCreateTransactionException("down").isDatabaseFailure() shouldBe true
        TransactionSystemException("commit failed").isDatabaseFailure() shouldBe true
    }

    "does not count transaction misuse, which is a bug here, not the database" {
        IllegalTransactionStateException("no transaction").isDatabaseFailure() shouldBe false
    }

    "does not count anything else" {
        IllegalStateException("bug").isDatabaseFailure() shouldBe false
    }
})
