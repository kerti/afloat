package dev.kerti.afloat.auth.data

import jakarta.persistence.Column
import jakarta.persistence.Entity
import jakarta.persistence.Id
import jakarta.persistence.Table
import org.hibernate.annotations.SQLRestriction
import java.math.BigDecimal
import java.time.Instant
import java.time.LocalTime
import java.util.UUID

@Entity
@Table(name = "households")
@SQLRestriction("deleted_at IS NULL")
class Household(
    @Id @Column(name = "id") val id: UUID,
    @Column(name = "display_name") val displayName: String,
    @Column(name = "reporting_currency") val reportingCurrency: String,
    @Column(name = "period_start_day") val periodStartDay: Int,
    @Column(name = "day_starts_at") val dayStartsAt: LocalTime,
    @Column(name = "expected_monthly_income", precision = 20, scale = 4) val expectedMonthlyIncome: BigDecimal?,
    @Column(name = "allowance_mode") val allowanceMode: String,
    @Column(name = "created_at") val createdAt: Instant,
    @Column(name = "updated_at") val updatedAt: Instant,
    @Column(name = "deleted_at") val deletedAt: Instant? = null,
)
