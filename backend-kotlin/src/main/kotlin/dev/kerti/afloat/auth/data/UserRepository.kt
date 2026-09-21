package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import java.util.UUID

interface UserRepository : JpaRepository<User, UUID> {
    // The @SQLRestriction adds deleted_at IS_NULL; the unique index is
    // lower(email) WHERE deleted_at IS NULL, so the match expression agrees.
    fun findByEmail(email: String): User?
}
