package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import org.springframework.data.domain.Persistable
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "sessions")
class Session(
    @Id @Column(name = "id") private val sessionId: String,
    @Column(name = "user_id") val userId: UUID,
    @Column(name = "created_at") val createdAt: Instant,
    @Column(name = "expires_at") val expiresAt: Instant,
    @Column(name = "last_seen_at") val lastSeenAt: Instant,
    @Column(name = "user_agent") val userAgent: String?,
) : Persistable<String> {
    override fun getId(): String = sessionId
    override fun isNew(): Boolean = true
}
