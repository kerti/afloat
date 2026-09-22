package dev.kerti.afloat

import dev.kerti.afloat.testsupport.DatabaseSpec
import io.kotest.core.extensions.ApplyExtension
import io.kotest.extensions.spring.SpringExtension
import org.springframework.boot.test.context.SpringBootTest

@ApplyExtension(SpringExtension::class)
@SpringBootTest
open class AfloatApplicationSpec : DatabaseSpec() {

	init {
		// contextLoads
		// hibernateValidatesAgainstFlywaySchema
		//
		// Both, from one boot: the context starts with ddl-auto=validate, so an
		// entity that disagrees with the Flyway schema fails HERE rather than at
		// the first query that touches the column. Trivially true while there
		// are few entities; the day one is wrong, this is the test that says so.
		"contextLoads" {
			// the Spring context boots against the migrated shared database
		}
	}
}
