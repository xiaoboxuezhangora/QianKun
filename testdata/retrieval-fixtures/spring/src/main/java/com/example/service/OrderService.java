package com.example.service;

import org.springframework.stereotype.Service;
import com.example.repository.OrderRepository;

@Service
public class OrderService {
    private final OrderRepository orderRepository;

    public OrderService(OrderRepository orderRepository) {
        this.orderRepository = orderRepository;
    }

    public Object findAll() {
        return orderRepository.findAll();
    }

    public Object create() {
        return null;
    }
}
