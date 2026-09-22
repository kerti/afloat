package dev.kerti.afloat

import dev.kerti.afloat.testsupport.ZeroDisableDatabaseInitializer
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [ZeroDisableDatabaseInitializer::class])
class FlywayDisabledByZeroValueSpec : FlywayEnablementParitySpec(
    runsMigration = false,
    rowName = "flywayDoesNotRunWhenAutoMigrateSpelledAsZero",
)