package dev.kerti.afloat

import dev.kerti.afloat.testsupport.NoMigrateDatabaseInitializer
import org.springframework.test.context.ContextConfiguration

@ContextConfiguration(initializers = [NoMigrateDatabaseInitializer::class])
class FlywayDisabledSpec : FlywayEnablementParitySpec(
    runsMigration = false,
    rowName = "flywayDoesNotRunWhenAutoMigrateDisabled",
)