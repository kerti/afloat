package dev.kerti.afloat.config

import io.kotest.assertions.throwables.shouldThrow
import io.kotest.core.spec.style.StringSpec
import io.kotest.datatest.withData
import io.kotest.matchers.nulls.shouldBeNull
import io.kotest.matchers.shouldBe
import org.springframework.mock.env.MockEnvironment

data class AutoMigrateCase(val name: String, val autoMigrate: String, val expected: String)

// Pins the one job of AutoMigrateFlywayPropertySource: whatever spelling the
// §12 contract accepts for AUTO_MIGRATE, spring.flyway.enabled sees the
// canonical "true"/"false" Boot's @ConditionalOnBooleanProperty will match.
open class AutoMigrateFlywayPropertySourceSpec : StringSpec({

    withData(
        nameFn = { it.name },
        listOf(
            AutoMigrateCase("spelling 1", "1", "true"),
            AutoMigrateCase("spelling t", "t", "true"),
            AutoMigrateCase("spelling T", "T", "true"),
            AutoMigrateCase("spelling TRUE", "TRUE", "true"),
            AutoMigrateCase("spelling True", "True", "true"),
            AutoMigrateCase("spelling true", "true", "true"),
            AutoMigrateCase("spelling 0", "0", "false"),
            AutoMigrateCase("spelling f", "f", "false"),
            AutoMigrateCase("spelling F", "F", "false"),
            AutoMigrateCase("spelling FALSE", "FALSE", "false"),
            AutoMigrateCase("spelling False", "False", "false"),
            AutoMigrateCase("spelling false", "false", "false"),
        )
    ) { (_, autoMigrate, expected) ->
        val source = AutoMigrateFlywayPropertySource(MockEnvironment().withProperty("AUTO_MIGRATE", autoMigrate))
        source.getProperty("spring.flyway.enabled") shouldBe expected
    }

    "absent AUTO_MIGRATE leaves spring.flyway.enabled unresolved (Boot's default true applies)" {
        val source = AutoMigrateFlywayPropertySource(MockEnvironment())
        source.getProperty("spring.flyway.enabled").shouldBeNull()
    }

    "resolves nothing but spring.flyway.enabled" {
        val source = AutoMigrateFlywayPropertySource(MockEnvironment().withProperty("AUTO_MIGRATE", "1"))
        source.getProperty("AUTO_MIGRATE").shouldBeNull()
        source.getProperty("DATABASE_URL").shouldBeNull()
    }

    "invalid AUTO_MIGRATE fails with the AppConfig message" {
        val source = AutoMigrateFlywayPropertySource(MockEnvironment().withProperty("AUTO_MIGRATE", "banana"))
        val ex = shouldThrow<ConfigException> { source.getProperty("spring.flyway.enabled") }
        ex.message shouldBe "AUTO_MIGRATE: 'banana' is not a boolean"
    }
})