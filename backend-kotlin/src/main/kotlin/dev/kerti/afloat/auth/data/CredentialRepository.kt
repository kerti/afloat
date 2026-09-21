package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import java.util.UUID

interface CredentialRepository : JpaRepository<Credential, UUID> {
    fun findByUserId(userId: UUID): Credential?
}
