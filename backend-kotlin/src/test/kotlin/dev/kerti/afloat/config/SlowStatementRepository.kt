package dev.kerti.afloat.config

import dev.kerti.afloat.auth.data.Session
import org.springframework.data.jpa.repository.Query
import org.springframework.data.repository.Repository
import org.springframework.data.repository.query.Param
import org.springframework.transaction.annotation.Transactional

@Transactional(readOnly = true)
interface SlowStatementRepository : Repository<Session, String> {

    @Query(nativeQuery = true, value = "SELECT 1 FROM pg_sleep(:seconds)")
    fun sleep(@Param("seconds") seconds: Int): Int
}
