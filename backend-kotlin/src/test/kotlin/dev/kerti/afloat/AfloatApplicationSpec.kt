package dev.kerti.afloat

import io.kotest.core.extensions.ApplyExtension
import io.kotest.core.spec.style.StringSpec
import io.kotest.extensions.spring.SpringExtension
import org.springframework.boot.test.context.SpringBootTest

@ApplyExtension(SpringExtension::class)
@SpringBootTest
open class AfloatApplicationSpec : StringSpec({

	"contextLoads" {
		// the Spring context boots, or this spec fails at boot
	}
})
