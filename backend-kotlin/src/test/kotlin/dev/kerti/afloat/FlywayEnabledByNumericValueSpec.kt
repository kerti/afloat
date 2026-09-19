package dev.kerti.afloat

import dev.kerti.afloat.testsupport.NumericEnableDatabaseInitializer
import org.springframework.test.context.ContextConfiguration

// The reported regression, end-to-end: AUTO_MIGRATE=1 passes AppConfig's
// Go-parity parse, and must turn Flyway ON, not off. An explicit spring.flyway
// .enabled override is deliberately absent from every initializer here.
@ContextConfiguration(initializers = [NumericEnableDatabaseInitializer::class])
class FlywayEnabledByNumericValueSpec : FlywayEnablementParitySpec(
    runsMigration = true,
    rowName = "flywayMigratesWhenAutoMigrateSpelledAsOne",
)