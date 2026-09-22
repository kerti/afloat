package dev.kerti.afloat

import dev.kerti.afloat.testsupport.TEnableDatabaseInitializer
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [TEnableDatabaseInitializer::class])
class FlywayEnabledByTValueSpec : FlywayEnablementParitySpec(
    runsMigration = true,
    rowName = "flywayMigratesWhenAutoMigrateSpelledAsT",
)