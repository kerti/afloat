package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "credentials")
class Credential(
    @Id @Column(name = "user_id") val userId: UUID,
    @Column(name = "password_hash") val passwordHash: String,
    @Column(name = "created_at") val createdAt: Instant,
    @Column(name = "updated_at") val updatedAt: Instant,
)
