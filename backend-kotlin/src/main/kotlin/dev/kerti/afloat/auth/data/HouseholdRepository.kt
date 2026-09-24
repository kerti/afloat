package dev.kerti.afloat.auth.data

import org.springframework.data.jpa.repository.JpaRepository
import org.springframework.transaction.annotation.Transactional
import java.util.UUID

@Transactional(readOnly = true)
interface HouseholdRepository : JpaRepository<Household, UUID>
