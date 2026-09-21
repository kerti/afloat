package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant

@Entity
@Table(name = "login_attempts")
class LoginAttempt(
    @Id @Column(name = "key") val key: String,
    @Column(name = "failure_count") val failureCount: Int,
    @Column(name = "backoff_until") val backoffUntil: Instant?,
    @Column(name = "updated_at") val updatedAt: Instant,
)
