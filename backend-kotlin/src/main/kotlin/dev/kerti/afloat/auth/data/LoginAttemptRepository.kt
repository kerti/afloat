package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Propagation
import org.springframework.transaction.annotation.Transactional
import java.time.Instant

interface LoginAttemptRepository : JpaRepository<LoginAttempt, String> {
    // The pair of keys (email + ip) is checked in a single read. MAX over no
    // rows yields one null row, so a null return means "no active backoff".
    @Query(
        """
        SELECT MAX(l.backoffUntil)
        FROM LoginAttempt l
        WHERE
            l.key IN :keys
            AND l.backoffUntil > :now
    """
    )
    fun activeBackoff(
        @Param("keys") keys: Collection<String>,
        @Param("now") now: Instant
    ): Instant?

    // Exponential, capped: 2^n seconds from the first failure, never longer
    // than the cap. Backoff, never a hard lockout: a lockout on a self-hosted
    // household app locks the household out of its own data. Mirrors the sqlc
    // query in backend/internal/db/auth.sql exactly.
    @Transactional(propagation = Propagation.REQUIRES_NEW)
    @Modifying(clearAutomatically = true)
    @Query(nativeQuery = true, value = """
        INSERT INTO login_attempts (key, failure_count, backoff_until)
        VALUES (:key, 1, now() + make_interval(secs => :firstBackoff))
        ON CONFLICT (key) DO UPDATE SET
            failure_count = login_attempts.failure_count + 1,
            backoff_until = now() + least(
                make_interval(secs => :firstBackoff) * pow(2, login_attempts.failure_count),
                make_interval(secs => :maxBackoff)
            ),
            updated_at = now()
    """)
    fun recordFailure(
        @Param("key") key: String,
        @Param("firstBackoff") firstBackoff: Long,
        @Param("maxBackoff") maxBackoff: Long,
    )

    // One statement, like Go's DELETE ... = ANY($1): the derived form
    // would load every row and delete them one at a time.
    @Modifying(clearAutomatically = true)
    @Query("DELETE FROM LoginAttempt l WHERE l.key IN :keys")
    fun deleteByKeyIn(@Param("keys") keys: Collection<String>)
}
