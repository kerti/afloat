package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import org.hibernate.annotations.SQLRestriction
import java.time.Instant
import java.util.UUID

@Entity
@Table(name = "users")
@SQLRestriction("deleted_at IS NULL")
class User(
    @Id @Column(name = "id") val id: UUID,
    @Column(name = "household_id") val householdId: UUID,
    @Column(name = "email") val email: String,
    @Column(name = "display_name") val displayName: String,
    @Column(name = "locale") val locale: String,
    @Column(name = "time_zone") val timeZone: String,
    @Column(name = "created_at") val createdAt: Instant,
    @Column(name = "updated_at") val updatedAt: Instant,
    @Column(name = "deleted_at") val deletedAt: Instant? = null,
)
