package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Transactional
import java.util.UUID

@Transactional(readOnly = true)
interface UserRepository : JpaRepository<User, UUID> {
    // lower(email), explicitly — NOT a derived findByEmail.
    //
    // The column is plain `text`; the only thing that lower-cases anything is
    // the unique index, which is on `lower(email)` (V0001__baseline.sql). So a
    // row spelled `User@Example.com` is legal, and a derived method comparing
    // `email = :email` would miss it while Go's
    // `lower(email) = lower($1)` (backend/queries/auth.sql) finds it — the two
    // backends disagreeing about who exists, which no contract test can see.
    //
    // The caller has already normalised, so only the column side needs
    // lowering; matching the index expression is what keeps a lookup from
    // disagreeing with what the index considers a duplicate. @SQLRestriction
    // adds `deleted_at IS NULL`, the other half of that index's predicate.
    @Query("SELECT u FROM User u WHERE lower(u.email) = :email")
    fun findByEmail(@Param("email") email: String): User?
}
