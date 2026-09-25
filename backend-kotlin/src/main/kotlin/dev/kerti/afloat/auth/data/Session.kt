package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import org.hibernate.annotations.Generated
import org.hibernate.generator.EventType
import org.springframework.data.domain.Persistable
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "sessions")
class Session(
    @Id @Column(name = "id") private val sessionId: String,
    @Column(name = "user_id") val userId: UUID,
    @Column(name = "expires_at") val expiresAt: Instant,
    @Column(name = "user_agent") val userAgent: String?,
) : Persistable<String> {
    // Left to the schema's DEFAULT now(), never written from the app clock:
    // findLive compares created_at against the database's now(), and the two
    // sides of that comparison must come from one clock or skew moves the
    // absolute cap (#25, BOOTSTRAP.md §5.1). Go's CreateSession omits both
    // columns for the same reason. Read back after the insert.
    @Generated(event = [EventType.INSERT])
    @Column(name = "created_at", insertable = false, updatable = false)
    var createdAt: Instant? = null
        protected set

    @Generated(event = [EventType.INSERT])
    @Column(name = "last_seen_at", insertable = false, updatable = false)
    var lastSeenAt: Instant? = null
        protected set

    override fun getId(): String = sessionId
    override fun isNew(): Boolean = true
}
