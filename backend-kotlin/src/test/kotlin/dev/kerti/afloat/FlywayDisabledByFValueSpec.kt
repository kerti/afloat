package dev.kerti.afloat

import dev.kerti.afloat.testsupport.FDisableDatabaseInitializer
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [FDisableDatabaseInitializer::class])
class FlywayDisabledByFValueSpec : FlywayEnablementParitySpec(
    runsMigration = false,
    rowName = "flywayDoesNotRunWhenAutoMigrateSpelledAsF",
)