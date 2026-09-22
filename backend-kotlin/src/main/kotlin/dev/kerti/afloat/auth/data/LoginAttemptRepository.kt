package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Transactional

interface LoginAttemptRepository : JpaRepository<LoginAttempt, String> {
    // The pair of keys (email + ip) is checked in a single read, against the
    // database's own now() like recordFailure writes with: an app-side instant
    // would skew the throttle and Retry-After by any clock drift between the two
    // hosts. Returns the seconds left in the longest active backoff; the
    // aggregate over no rows is one null row, so null means "no active backoff".
    @Query(
        nativeQuery = true,
        value = """
        SELECT extract(epoch FROM (max(backoff_until) - now()))::float8
        FROM login_attempts
        WHERE key IN (:keys) AND backoff_until > now()
    """
    )
    fun activeBackoffSeconds(@Param("keys") keys: Collection<String>): Double?

    // Exponential, capped: 2^n seconds from the first failure, never longer
    // than the cap. Backoff, never a hard lockout: a lockout on a self-hosted
    // household app locks the household out of its own data. Mirrors the sqlc
    // query in backend/queries/auth.sql exactly. Its own transaction, and
    // AuthService.login holds none: a failure here aborts only this statement.
    @Transactional
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
    @Transactional
    @Modifying(clearAutomatically = true)
    @Query("DELETE FROM LoginAttempt l WHERE l.key IN :keys")
    fun deleteByKeyIn(@Param("keys") keys: Collection<String>)
}
