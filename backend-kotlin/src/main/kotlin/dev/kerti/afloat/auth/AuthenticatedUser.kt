package dev.kerti.afloat.auth

import java.util.UUID

data class AuthenticatedUser(
    val userId: UUID,
    val householdId: UUID,
    val email: String,
)
