package dev.kerti.afloat.config

import io.kotest.core.spec.style.StringSpec
import io.kotest.matchers.shouldBe

open class AppConfigEnvNamesSpec : StringSpec({

    "ENV_NAMES match the placeholders in application.yaml" {
        // Whole file, not the afloat: subtree: PORT lives under server:.
        val yaml = requireNotNull(javaClass.getResource("/application.yaml")).readText()
        val scraped = Regex("""\$\{([A-Z0-9_]+)""")
            .findAll(yaml)
            .map { it.groupValues[1] }
            .toSet()
        AppConfig.ENV_NAMES.toSet() shouldBe scraped
    }
})
