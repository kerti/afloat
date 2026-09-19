package dev.kerti.afloat

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.core.extensions.ApplyExtension
import io.kotest.extensions.spring.SpringExtension
import org.springframework.boot.test.context.SpringBootTest

@ApplyExtension(SpringExtension::class)
@SpringBootTest
open class AfloatApplicationSpec : DatabaseSpec() {

	init {
		"contextLoads" {
			// the Spring context boots against the migrated shared database
		}
	}
}
