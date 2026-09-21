package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Propagation
import org.springframework.transaction.annotation.Transactional
import java.time.Instant

interface SessionRepository : JpaRepository<Session, String> {
    // Both lifetimes enforced here: expires_at is the sliding window,
    // created_at is the absolute cap that stops a stolen cookie living forever
    // (BOOTSTRAP §5.1).
    @Query(
        """
        SELECT s FROM Session s
        WHERE s.id = :id AND s.expiresAt > :now AND s.createdAt > :minCreatedAt
    """
    )
    fun findLive(
        @Param("id") id: String,
        @Param("now") now: Instant,
        @Param("minCreatedAt") minCreatedAt: Instant
    ): Session?

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    @Modifying(clearAutomatically = true)
    @Query(
        """
        UPDATE Session s SET s.lastSeenAt = :now, s.expiresAt = :expiresAt WHERE s.id = :id
    """
    )
    fun touch(
        @Param("id") id: String,
        @Param("now") now: Instant,
        @Param("expiresAt") expiresAt: Instant
    )

    // A bulk delete, so an absent row is a no-op rather than an exception:
    // logout must stay idempotent.
    @Transactional
    @Modifying
    @Query(
        """
        DELETE FROM Session s WHERE s.id = :id
    """
    )
    fun deleteRow(@Param("id") id: String)
}
