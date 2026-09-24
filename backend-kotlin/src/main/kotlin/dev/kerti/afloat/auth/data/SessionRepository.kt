package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Modifying
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Transactional
import java.time.Instant

@Transactional(readOnly = true)
interface SessionRepository : JpaRepository<Session, String> {
    // Both lifetimes enforced here: expires_at is the sliding window,
    // created_at is the absolute cap that stops a stolen cookie living forever
    // (BOOTSTRAP §5.1).
    //
    // now() is the database's, like Go's GetLiveSession, so app/DB clock skew
    // cannot lengthen or shorten either window.
    @Query(
        nativeQuery = true,
        value = """
        SELECT * FROM sessions
        WHERE id = :id
          AND expires_at > now()
          AND created_at > now() - make_interval(secs => :maxLifetimeSeconds)
    """
    )
    fun findLive(
        @Param("id") id: String,
        @Param("maxLifetimeSeconds") maxLifetimeSeconds: Long,
    ): Session?

    @Transactional
    @Modifying(clearAutomatically = true)
    @Query(
        nativeQuery = true,
        value = """
        UPDATE sessions SET last_seen_at = now(), expires_at = :expiresAt WHERE id = :id
    """
    )
    fun touch(
        @Param("id") id: String,
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
