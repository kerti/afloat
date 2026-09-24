package dev.kerti.afloat.auth

import org.springframework.dao.DataAccessException
import org.springframework.transaction.TransactionException
import org.springframework.transaction.TransactionUsageException

// Every repository is @Transactional, so its connection is taken when the
// transaction BEGINS, before any statement runs. A database that is down, or a
// pool with nothing left to lend, therefore arrives as
// CannotCreateTransactionException - a TransactionException, not a
// DataAccessException - and a catch written for DataAccessException alone
// misses exactly the outage it was written for (#23, #27).
//
// TransactionUsageException is the one TransactionException family that is not
// the database's doing: an illegal propagation or isolation setting is a bug in
// this code, and one treated as an outage would be logged and served as an
// unauthenticated request instead of failing where it can be seen.
internal fun RuntimeException.isDatabaseFailure(): Boolean =
    this is DataAccessException ||
        (this is TransactionException && this !is TransactionUsageException)
