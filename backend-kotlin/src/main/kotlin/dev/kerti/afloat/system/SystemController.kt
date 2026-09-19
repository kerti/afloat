package dev.kerti.afloat.system

import dev.kerti.afloat.api.SystemApi
import dev.kerti.afloat.api.model.Health
import dev.kerti.afloat.config.AppConfig
import org.springframework.http.HttpStatus
import org.springframework.http.ResponseEntity
import org.springframework.web.bind.annotation.RestController

@RestController
class SystemController(private val probe: DatabaseProbe, private val config: AppConfig) : SystemApi {
    override fun getHealth(): ResponseEntity<Health> {
        val ok = probe.isReachable()
        val body = Health(if (ok) Health.Status.ok else Health.Status.degraded, config.version)
        return if (ok) ResponseEntity.ok(body) else ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE).body(body)
    }
}
